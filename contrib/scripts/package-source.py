#!/usr/bin/env python3
"""Export a committed Ark revision and its dependency sources for redistribution.

Requires Python >= 3.12, Go, Git, and (for arkd/images) Cargo or Docker.
Outputs contain no checkout, cache, or home-directory paths.
"""

import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import zipfile


ROOT = Path(__file__).resolve().parents[2]
WASMVM = "github.com/CosmWasm/wasmvm/v3"
LEGAL = ("LICENSE", "COPYING", "NOTICE", "THIRD_PARTY_NOTICES.md")


def run(*args, cwd=None, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True)


def objects(text):
    decoder = json.JSONDecoder()
    pos = 0
    while pos < len(text):
        if text[pos].isspace():
            pos += 1
            continue
        value, pos = decoder.raw_decode(text, pos)
        yield value


def escaped(value):
    return "".join("!" + c.lower() if c.isupper() else c for c in value)


def effective(module):
    module = module.get("Replace", module)
    if not module.get("Version"):
        raise ValueError("local module replacements cannot be redistributed: " + module["Path"])
    return module


def proxy_path(module):
    return Path(escaped(module["Path"])) / "@v" / escaped(module["Version"])


def copy_file(source, target):
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, target)


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def pack(directory, output, timestamp):
    """Only ordinary files/directories; never follow symlinks into a checkout."""
    with output.open("wb") as raw:
        import gzip
        with gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=timestamp) as gz:
            with tarfile.open(fileobj=gz, mode="w") as archive:
                for path in sorted(directory.rglob("*")):
                    if path.is_symlink() or not (path.is_file() or path.is_dir()):
                        raise ValueError("unsupported source entry: " + str(path.relative_to(directory)))
                    info = archive.gettarinfo(str(path), str(path.relative_to(directory)))
                    info.uid = info.gid = 0
                    info.uname = info.gname = ""
                    info.mtime = timestamp
                    info.mode = 0o755 if path.is_dir() or path.stat().st_mode & 0o111 else 0o644
                    if path.is_file():
                        with path.open("rb") as stream:
                            archive.addfile(info, stream)
                    else:
                        archive.addfile(info)


def extract_module(archive_path, destination):
    with zipfile.ZipFile(archive_path) as archive:
        for name in archive.namelist():
            if name.endswith("/"):
                continue
            # A Go zip's prefix includes the full module path, not just its first segment.
            prefix_end = name.find("/", name.index("@"))
            relative = Path(name[prefix_end + 1:])
            if relative.is_absolute() or ".." in relative.parts:
                raise ValueError("unsafe module archive entry")
            target = destination / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(archive.read(name))


def module_sources(ark, bundle, kind, env):
    graph = list(objects(run("go", "list", "-m", "-json", "all", cwd=ark, env=env)))
    modules = {}
    for module in graph:
        if not module.get("Main"):
            m = effective(module)
            modules[(m["Path"], m["Version"])] = m
    # Keep the whole module graph's .mod metadata, but only download source zips for
    # packages used by the published target matrix. This avoids bundling unrelated tools.
    for module in modules.values():
        # Lazy graph entries whose go.mod was never loaded are not needed for
        # this build. Selected packages below always supply their own metadata.
        if not module.get("GoMod"):
            continue
        target = bundle / "go-proxy" / proxy_path(module)
        copy_file(module["GoMod"], Path(str(target) + ".mod"))
        info = Path(module["GoMod"]).with_suffix(".info")
        if info.exists():
            copy_file(info, Path(str(target) + ".info"))
    selected = {}
    targets = [("linux", a, "1", "netgo,ledger,muslc", "arkd") for a in ("amd64", "arm64")] if kind != "pricefeed" else []
    if kind in ("pricefeed", "image"):
        targets += [(o, a, "0", "netgo", "pricefeed") for o in ("linux", "darwin") for a in ("amd64", "arm64")]
    for goos, arch, cgo, tags, binary in targets:
        target_env = dict(env, GOOS=goos, GOARCH=arch, CGO_ENABLED=cgo)
        packages = objects(run("go", "list", "-deps", "-json", "-tags", tags, "./cmd/" + binary, cwd=ark, env=target_env))
        for package in packages:
            module = package.get("Module")
            if module and not module.get("Main"):
                m = effective(module)
                selected[(m["Path"], m["Version"])] = m
    records = []
    downloads = {}
    for key, module in sorted(selected.items()):
        name = "@".join(key)
        data = json.loads(run("go", "mod", "download", "-json", name, cwd=ark, env=env))
        if data.get("Error") or not data.get("Sum"):
            raise ValueError("source download failed: " + name)
        # Both the module tree and its metadata must match this revision's go.sum.
        sums = (ark / "go.sum").read_text().splitlines()
        for suffix, field in (("", "Sum"), ("/go.mod", "GoModSum")):
            expected = f"{key[0]} {key[1]}{suffix} {data[field]}"
            if expected not in sums:
                raise ValueError("dependency lacks a recorded checksum: " + name + suffix)
        target = bundle / "go-proxy" / proxy_path(module)
        for field, suffix in (("Zip", ".zip"), ("GoMod", ".mod"), ("Info", ".info")):
            copy_file(data[field], Path(str(target) + suffix))
        downloads[key[0]] = data
        notices = bundle / "notices" / "go" / name
        with zipfile.ZipFile(data["Zip"]) as archive:
            for member in archive.namelist():
                basename = Path(member).name.upper()
                if member.endswith("/") or not basename.startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS", "AUTHORS")):
                    continue
                relative = member[member.find("/", member.index("@")) + 1:]
                path = Path(relative)
                if path.is_absolute() or ".." in path.parts:
                    raise ValueError("unsafe notice archive path")
                output = notices / path
                output.parent.mkdir(parents=True, exist_ok=True)
                output.write_bytes(archive.read(member))
        records.append({"path": key[0], "version": key[1], "sum": data["Sum"], "go_mod_sum": data["GoModSum"]})
    return records, downloads, targets


def native_sources(bundle, downloads):
    wasm = bundle / "native" / "wasmvm"
    extract_module(downloads[WASMVM]["Zip"], wasm)
    manifest = wasm / "libwasmvm"
    lock_hash = digest(manifest / "Cargo.lock")
    if shutil.which("cargo"):
        config = run("cargo", "vendor", "--locked", "--versioned-dirs", "vendor", cwd=manifest)
    else:
        image = run(str(ROOT / "contrib/scripts/verify-source.sh"), "--prepare").strip().splitlines()[-1]
        config = run("docker", "run", "--rm", "-v", str(manifest) + ":/crate", "-w", "/crate", image,
                     "cargo", "vendor", "--locked", "--versioned-dirs", "vendor")
    if digest(manifest / "Cargo.lock") != lock_hash:
        raise ValueError("cargo vendor changed the locked dependency graph")
    (manifest / ".cargo").mkdir(exist_ok=True)
    (manifest / ".cargo/config.toml").write_text(config)
    for path in (bundle / "native").rglob("*"):
        if path.is_file() and path.name.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS", "AUTHORS")):
            copy_file(path, bundle / "notices" / path.relative_to(bundle))
    return {"wasmvm": downloads[WASMVM]["Version"], "cargo_lock_sha256": lock_hash}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--kind", choices=("arkd", "pricefeed", "image"), required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--image-context", type=Path)
    args = parser.parse_args()
    if args.image_context and args.kind != "image":
        parser.error("--image-context requires --kind image")
    run("git", "diff", "--exit-code", "HEAD", cwd=ROOT)
    revision = run("git", "rev-parse", "HEAD", cwd=ROOT).strip()
    if not re.fullmatch(r"[0-9a-f]{40,64}", revision):
        raise ValueError("invalid source revision")
    timestamp = int(run("git", "show", "-s", "--format=%ct", "HEAD", cwd=ROOT))
    output = args.output.resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, GOENV="off", GOWORK="off", GOFLAGS="-mod=readonly", GOTOOLCHAIN="local")
    with tempfile.TemporaryDirectory(prefix="ark-source-") as tmp:
        bundle = Path(tmp) / "bundle"
        ark = bundle / "ark"
        ark.mkdir(parents=True)
        data = subprocess.check_output(["git", "archive", "--format=tar", revision], cwd=ROOT)
        with tarfile.open(fileobj=io.BytesIO(data)) as archive:
            archive.extractall(ark, filter="data")
        for filename in LEGAL:
            if not (ark / filename).is_file():
                raise ValueError(filename + " must be committed before preparing a release")
        original_module_files = {name: (ark / name).read_bytes() for name in ("go.mod", "go.sum")}
        print("Collecting Go sources for " + args.kind, flush=True)
        modules, downloads, targets = module_sources(ark, bundle, args.kind, env)
        for name, original in original_module_files.items():
            if (ark / name).read_bytes() != original:
                raise ValueError("source resolution changed " + name)
        native = native_sources(bundle, downloads) if args.kind != "pricefeed" else {}
        goroot = Path(run("go", "env", "GOROOT", cwd=ark, env=env).strip())
        for name in ("LICENSE", "PATENTS"):
            if (goroot / name).exists():
                copy_file(goroot / name, bundle / "notices" / "go-toolchain" / name)
        manifest = {"format": 1, "revision": revision, "kind": args.kind,
                    "go": run("go", "version", cwd=ark, env=env).strip(),
                    "targets": targets, "modules": modules, "native": native}
        (bundle / "SOURCE-MANIFEST.json").write_text(json.dumps(manifest, indent=2) + "\n")
        (bundle / "README.txt").write_text(
            "Ark corresponding source\n\nRevision: " + revision + "\n"
            "Licensing and build instructions: ark/THIRD_PARTY_NOTICES.md\n"
            "Full Go dependency source and notices: go-proxy/**/*.zip\n"
            "Extracted Go notices: notices/\nNative sources and notices: native/ (node bundles)\n"
            "No Git history is required. Go's standard toolchain and operating-system build tools are prerequisites.\n")
        # Check revision and tracked content again after potentially lengthy downloads.
        if run("git", "rev-parse", "HEAD", cwd=ROOT).strip() != revision:
            raise ValueError("HEAD changed while packaging sources")
        run("git", "diff", "--exit-code", "HEAD", cwd=ROOT)
        pending = output.with_suffix(output.suffix + ".tmp")
        pack(bundle, pending, timestamp)
        pending.replace(output)
        Path(str(output) + ".sha256").write_text(digest(output) + "  " + output.name + "\n")
        notices_archive = output.with_name(output.name.removesuffix(".tar.gz") + "-notices.tar.gz")
        pack(bundle / "notices", notices_archive, timestamp)
        if args.image_context:
            context = args.image_context.resolve()
            if context.exists():
                raise ValueError("image context already exists; choose an empty destination")
            shutil.copytree(ark, context)
            copy_file(output, context / "corresponding-source.tar.gz")
            copy_file(notices_archive, context / "third-party-notices.tar.gz")
    print(output)


if __name__ == "__main__":
    main()
