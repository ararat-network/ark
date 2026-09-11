#!/usr/bin/env python3
"""GoReleaser's node build tool: export a verified container build and runtime sources."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import struct
import subprocess
import sys
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[2]
DOCKERFILE = "contrib/images/arkd-env/Dockerfile"
REQUEST = ".ark-release-request.json"
BINARY = "/work/build/release/arkd"
MAP = "/work/build/release.link-map"


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs)


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def load_collector(root=ROOT):
    spec = importlib.util.spec_from_file_location("os_source", root / "contrib/scripts/package-os-source.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def build_request(args, env, root):
    if env.get("GOOS") != "linux" or env.get("GOARCH") not in ("amd64", "arm64"):
        raise ValueError("release builds require Linux amd64 or arm64")
    if env.get("CGO_ENABLED") != "1":
        raise ValueError("the node requires CGO_ENABLED=1")
    if not args or args[0] != "build" or args[-1] != "./cmd/arkd":
        raise ValueError("expected GoReleaser's node build command")
    if args.count("-o") != 1:
        raise ValueError("expected one explicit build output")
    index = args.index("-o") + 1
    if index >= len(args) - 1:
        raise ValueError("missing build output")
    output = Path(args[index])
    output = (root / output).resolve()
    if not output.is_relative_to((root / "dist").resolve()) or output.name != "arkd":
        raise ValueError("build output must be an arkd binary inside dist/")
    forwarded = list(args)
    forwarded[index] = BINARY
    if any(str(root) in arg or "/Users/" in arg or "\x00" in arg for arg in forwarded):
        raise ValueError("build arguments contain a host path or NUL")
    if not any(MAP in arg and "-static" in arg for arg in forwarded):
        raise ValueError("release build must record its static linker inputs")
    target = env["GOARCH"]
    cpu_key = "GOAMD64" if target == "amd64" else "GOARM64"
    cpu = env.get(cpu_key) or ("v1" if target == "amd64" else "v8.0")
    pattern = r"v[1-4]" if target == "amd64" else r"v[89]\.[0-9](?:,(?:lse|crypto))*"
    if not re.fullmatch(pattern, cpu):
        raise ValueError("invalid CPU target")
    return dict(args=forwarded, env=dict(GOOS="linux", GOARCH=target, CGO_ENABLED="1", **{cpu_key: cpu})), output


def unpack_source(bundle, revision, destination):
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("invalid revision")
    expected = Path(str(bundle) + ".sha256").read_text().split()[0]
    actual = digest(bundle)
    if actual != expected:
        raise ValueError("application source checksum mismatch")
    with tarfile.open(bundle) as archive:
        manifest = json.load(archive.extractfile("SOURCE-MANIFEST.json"))
        if manifest["kind"] != "arkd" or manifest["revision"] != revision:
            raise ValueError("application source kind/revision mismatch")
        seen = set()
        for item in archive:
            path = PurePosixPath(item.name)
            if path.is_absolute() or ".." in path.parts or not (item.isfile() or item.isdir()):
                raise ValueError("unsafe application source entry")
            if item.name in seen:
                raise ValueError("duplicate application source entry")
            seen.add(item.name)
            if not path.parts or path.parts[0] != "ark":
                continue
            relative = path.relative_to("ark")
            if ".git" in relative.parts or relative.name == REQUEST:
                raise ValueError("unexpected private/build metadata in source export")
            dest = destination / relative
            if item.isdir():
                dest.mkdir(parents=True, exist_ok=True)
            else:
                dest.parent.mkdir(parents=True, exist_ok=True)
                with archive.extractfile(item) as src, dest.open("wb") as out:
                    shutil.copyfileobj(src, out)
                dest.chmod(0o755 if item.mode & 0o111 else 0o644)
    return actual


def check_binary(path, arch):
    with path.open("rb") as stream:
        header = stream.read(64)
        if len(header) != 64 or header[:6] != b"\x7fELF\x02\x01":
            raise ValueError("expected a 64-bit little-endian ELF binary")
        if struct.unpack_from("<H", header, 18)[0] != {"amd64": 62, "arm64": 183}[arch]:
            raise ValueError("binary architecture mismatch")
        offset = struct.unpack_from("<Q", header, 32)[0]
        entry_size, count = struct.unpack_from("<HH", header, 54)
        if entry_size != 56 or not 0 < count < 1024:
            raise ValueError("invalid ELF program headers")
        stream.seek(offset)
        table = stream.read(entry_size * count)
        if len(table) != entry_size * count:
            raise ValueError("truncated ELF program headers")
        if any(struct.unpack_from("<I", table, i * entry_size)[0] in (2, 3) for i in range(count)):
            raise ValueError("node is dynamically linked")


def inside():
    request = json.loads((ROOT / REQUEST).read_text())
    arch = request["env"]["GOARCH"]
    if run("go", "env", "GOHOSTARCH").strip() != arch:
        raise ValueError("builder architecture does not match requested target")
    Path(BINARY).parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, **request["env"], GOENV="off", GOTOOLCHAIN="local", GOWORK="off", CC="gcc")
    subprocess.run(["go", *request["args"]], cwd=ROOT, env=env, check=True)
    check_binary(Path(BINARY), arch)
    collector = load_collector()
    linked = collector.link_inputs(Path("/lib/apk/db/installed"), Path(MAP))
    collector.validate_static_inputs(linked)
    (ROOT / "build/static-runtime.json").write_text(json.dumps(linked, indent=2) + "\n")
    info = json.loads(run("go", "version", "-m", "-json", BINARY))
    record = dict(format=1, revision=request["revision"], target=request["env"],
                  build_args=request["args"], application_source_sha256=request["source_sha256"],
                  binary_sha256=digest(Path(BINARY)), go=info["GoVersion"],
                  static_runtime_sha256=digest(ROOT / "build/static-runtime.json"))
    (ROOT / "build/BUILD.json").write_text(json.dumps(record, indent=2) + "\n")


def verify_export(export, request, collector):
    binary = export / "arkd"
    arch = request["env"]["GOARCH"]
    check_binary(binary, arch)
    record = json.loads((export / "BUILD.json").read_text())
    expected = dict(revision=request["revision"], target=request["env"], build_args=request["args"],
                    application_source_sha256=request["source_sha256"], binary_sha256=digest(binary))
    if any(record.get(k) != v for k, v in expected.items()):
        raise ValueError("exported binary/source build identity mismatch")
    linked_file = export / "static-runtime.json"
    if record["static_runtime_sha256"] != digest(linked_file):
        raise ValueError("linked runtime metadata changed")
    linked = json.loads(linked_file.read_text())
    collector.validate_static_inputs(linked)
    architecture = "x86_64" if arch == "amd64" else "aarch64"
    if any(p["architecture"] != architecture for p in linked["packages"]):
        raise ValueError("runtime package architecture mismatch")
    runtime = export / "runtime-source"
    archive = runtime / "corresponding-source.tar.gz"
    if digest(archive) != (runtime / "corresponding-source.tar.gz.sha256").read_text().split()[0]:
        raise ValueError("runtime source checksum mismatch")
    with tempfile.TemporaryDirectory(prefix="ark-runtime-verify-") as tmp:
        root = Path(tmp)
        with tarfile.open(archive) as tar:
            for item in tar:
                collector.safe_path(item.name)
                if not (item.isfile() or item.isdir() or item.issym()):
                    raise ValueError("unsupported runtime archive entry")
                if item.issym():
                    collector.safe_path(item.linkname)
            tar.extractall(root, filter="data")
        collector.verify_tree(root)
        manifest = json.loads((root / "MANIFEST.json").read_text())
        if manifest.get("mode") != "static-only" or manifest["static_runtime"] != linked:
            raise ValueError("runtime sources do not cover this binary's linked inputs")
        if (root / "MANIFEST.json").read_bytes() != (runtime / "MANIFEST.json").read_bytes():
            raise ValueError("runtime manifest differs from archive")
        # Publish notices from the verified archive, never unchecked loose files.
        notices = export / "verified-notices"
        shutil.copytree(root / "notices", notices)
    record["runtime_source_sha256"] = digest(archive)
    return record


def host(args):
    request, output = build_request(args, os.environ, ROOT)
    subprocess.run(["git", "diff", "--exit-code", "HEAD"], cwd=ROOT, check=True)
    revision = run("git", "rev-parse", "HEAD", cwd=ROOT).strip()
    bundle = Path(os.environ["ARK_RELEASE_SOURCE"]).resolve()
    arch = request["env"]["GOARCH"]
    collector = load_collector()
    with tempfile.TemporaryDirectory(prefix="ark-release-build-") as tmp:
        work = Path(tmp)
        context = work / "context"
        request["source_sha256"] = unpack_source(bundle, revision, context)
        request["revision"] = revision
        # Require the adapter/recipe actually executing to be the committed versions.
        for name in ("contrib/scripts/release-build.py", "contrib/scripts/package-os-source.py", DOCKERFILE):
            if (ROOT / name).read_bytes() != (context / name).read_bytes():
                raise ValueError("release tooling differs from source bundle: " + name)
        (context / REQUEST).write_text(json.dumps(request, indent=2) + "\n")
        export = work / "export"
        subprocess.run(["docker", "build", "--platform", "linux/" + arch, "--target", "release-export",
                        "--file", str(context / DOCKERFILE), "--output", "type=local,dest=" + str(export),
                        str(context)], check=True)
        record = verify_export(export, request, collector)
        if run("git", "rev-parse", "HEAD", cwd=ROOT).strip() != revision:
            raise ValueError("HEAD changed during release build")
        subprocess.run(["git", "diff", "--exit-code", "HEAD"], cwd=ROOT, check=True)
        sources = ROOT / "build/source"
        sources.mkdir(parents=True, exist_ok=True)
        stem = f"arkd-runtime-source-{revision}-linux-{arch}"
        runtime = export / "runtime-source"
        archive = sources / (stem + ".tar.gz")
        shutil.copyfile(runtime / "corresponding-source.tar.gz", archive)
        (sources / (stem + ".tar.gz.sha256")).write_text(record["runtime_source_sha256"] + "  " + archive.name + "\n")
        shutil.copyfile(runtime / "MANIFEST.json", sources / (stem + "-manifest.json"))
        (sources / (stem + "-build.json")).write_text(json.dumps(record, indent=2) + "\n")
        collector.pack(export / "verified-notices", sources / (stem + "-notices.tar.gz"))
        output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(export / "arkd", output)
        output.chmod(0o755)
    print("Verified standalone build and runtime sources:", arch)


if __name__ == "__main__":
    if sys.argv[1:] == ["--inside"]:
        inside()
    else:
        host(sys.argv[1:])
