#!/usr/bin/env python3
"""Collect exact Alpine source recipes/archives in an unprivileged build stage."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import tarfile
import tempfile

APORTS = "https://github.com/alpinelinux/aports.git"
NOTICE = re.compile(r"license|licence|copying|copyright|notice|authors", re.I)
# CA certificate tooling carries its notices in these source files, rather than LICENSE.
EMBEDDED_NOTICES = {"ca-certificates": {"certdata.txt", "c_rehash.c", "update-ca.c", "mk-ca-bundle.pl"}}


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs)


def distfiles_mirror():
    """Alpine's source mirror for this release; abuild tries it before upstream."""
    major, minor = Path("/etc/alpine-release").read_text().split(".")[:2]
    return f"https://distfiles.alpinelinux.org/distfiles/v{major}.{minor}"


def safe_path(name):
    path = PurePosixPath(name)
    if not name or path.is_absolute() or ".." in path.parts or "\x00" in name:
        raise ValueError("unsafe archive/source path: " + name)
    return path


def packages(text):
    result = []
    for block in text.split("\n\n"):
        fields = {}
        files = []
        directory = ""
        for line in block.splitlines():
            if len(line) < 2 or line[1] != ":":
                continue
            key, value = line[0], line[2:]
            if key == "F":
                directory = value
            elif key == "R":
                files.append("/" + str(safe_path((directory + "/" if directory else "") + value)))
            elif key in "PVLocA":
                if key in fields:
                    raise ValueError("duplicate package field: " + key)
                fields[key] = value
        if "P" not in fields:
            continue
        if any(not fields.get(k) for k in "PVLocA"):
            raise ValueError("incomplete package metadata: " + fields["P"])
        if not re.fullmatch(r"[0-9a-f]{40}", fields["c"]):
            raise ValueError("invalid aports commit")
        for key in ("P", "o", "V", "A"):
            if not re.fullmatch(r"[A-Za-z0-9_+.-]+", fields[key]):
                raise ValueError("invalid package identity")
        result.append(dict(name=fields["P"], version=fields["V"], licence=fields["L"],
                           origin=fields["o"], commit=fields["c"], architecture=fields["A"], files=files))
    if not result:
        raise ValueError("empty package inventory")
    return result


def public(package):
    return {k: v for k, v in package.items() if k != "files"}


def source_key(package):
    return f"{package['origin']}-{package['version']}-{package['commit']}"


def group_sources(inventories):
    groups = {}
    identities = {}
    for scope, rows in inventories.items():
        for p in rows:
            identity = (scope, p["name"], p["architecture"])
            if identity in identities and identities[identity] != public(p):
                raise ValueError("conflicting package metadata")
            identities[identity] = public(p)
            key = source_key(p)
            groups.setdefault(key, []).append(dict(public(p), scope=scope))
    return groups


def link_inputs(database, link_map):
    rows = packages(database.read_text())
    owners = {}
    for p in rows:
        for path in p["files"]:
            owners[path] = p
            owners[os.path.realpath(path)] = p
    selected = {}
    inputs = []
    for name in re.findall(r"^LOAD (.+)$", link_map.read_text(), re.M):
        name = name.strip()
        # GNU ld's aarch64elf.em creates this fake input for generated branch veneers;
        # it is not a library read from disk. Keep all other relative inputs rejected.
        if name == "linker stubs" and {p["architecture"] for p in rows} <= {"aarch64", "noarch"}:
            continue
        if not name.startswith("/"):
            raise ValueError("unrecognised linker input: " + name)
        actual = os.path.realpath(name)
        owner = owners.get(actual) or owners.get(name)
        if owner:
            selected[owner["name"]] = public(owner)
            inputs.append(dict(path=actual, package=owner["name"]))
        elif re.fullmatch(r"/tmp/go-link-[^/]+/(go|[0-9]+)\.o", name):
            continue
        elif re.fullmatch(r"/(lib|usr/lib|usr/local/lib)/libwasmvm_muslc\.(x86_64|aarch64)\.a", actual):
            inputs.append(dict(component="wasmvm", source="application source bundle"))
        else:
            raise ValueError("linker input lacks source coverage: " + name)
    if not selected or not inputs:
        raise ValueError("linker map contains no system runtime inputs")
    return dict(packages=list(selected.values()), inputs=inputs)


def git_files(repo, commit, directory):
    run("git", "-C", str(repo), "fetch", "--quiet", "--depth=1", "--filter=blob:none", "origin", commit)
    raw = subprocess.check_output(["git", "-C", str(repo), "ls-tree", "-rz", commit, "--", directory])
    entries = []
    for record in raw.split(b"\x00"):
        if not record:
            continue
        info, name = record.split(b"\t", 1)
        mode, kind, oid = info.decode().split()
        path = safe_path(name.decode())
        if kind != "blob" or mode not in ("100644", "100755", "120000"):
            raise ValueError("unsupported recipe entry: " + str(path))
        entries.append((str(path), oid, mode))
    # Fetch recipe blobs together; one lazy network fetch per patch is very slow.
    if entries:
        subprocess.run(["git", "-C", str(repo), "fetch", "--quiet", "--no-tags",
                        "--no-write-fetch-head", "--stdin", "origin"],
                       input="\n".join(oid for _, oid, _ in entries), text=True, check=True)
    return entries


def recipe(repo, commit, origin, target):
    for section in ("main", "community"):
        prefix = f"{section}/{origin}"
        entries = git_files(repo, commit, prefix)
        if not entries:
            continue
        for name, oid, mode in entries:
            dest = target / PurePosixPath(name).relative_to(prefix)
            dest.parent.mkdir(parents=True, exist_ok=True)
            content = subprocess.check_output(["git", "-C", str(repo), "cat-file", "blob", oid])
            if mode == "120000":
                link = content.decode()
                safe_path(link)
                dest.symlink_to(link)
            else:
                dest.write_bytes(content)
                dest.chmod(0o755 if mode == "100755" else 0o644)
        for path in target.rglob("*"):
            if path.is_symlink():
                validate_link(path, target)
        if not (target / "APKBUILD").is_file():
            raise ValueError("missing APKBUILD")
        return prefix
    raise ValueError("source recipe not found: " + origin)


METADATA = '''
ark_source_metadata() {
    printf '%s\\0' "$pkgname" "$pkgver-r$pkgrel" "$source" "$sha512sums" "$sha256sums" > "$ARK_SOURCE_METADATA"
}
'''


def source_checks(metadata):
    origin, version, source, sha512, sha256 = metadata
    sums = {}
    algorithm = "sha512" if sha512.strip() else "sha256"
    for line in (sha512 if sha512.strip() else sha256).splitlines():
        if not line.strip():
            continue
        value, filename = line.split(None, 1)
        filename = filename.strip().lstrip("*")
        safe_path(filename)
        if not re.fullmatch(r"[0-9a-fA-F]{" + ("128" if algorithm == "sha512" else "64") + "}", value):
            raise ValueError("invalid source checksum")
        if filename in sums:
            raise ValueError("duplicate source checksum")
        sums[filename] = value.lower()
    result = []
    for uri in source.split():
        if "::" in uri:
            filename, address = uri.split("::", 1)
        else:
            address = uri
            filename = uri.rsplit("/", 1)[-1] if "://" in uri else uri
        safe_path(filename)
        if filename not in sums:
            raise ValueError("source lacks checksum: " + filename)
        result.append(dict(file=filename, url=address, algorithm=algorithm, digest=sums[filename]))
    return result


def check_file(path, record):
    h = hashlib.new(record["algorithm"])
    with path.open("rb") as stream:
        while chunk := stream.read(1024 * 1024):
            h.update(chunk)
    if h.hexdigest() != record["digest"]:
        raise ValueError("source checksum mismatch: " + record["file"])


def extract_notices(path, target, extra=(), omit_formats=()):
    target.mkdir(parents=True, exist_ok=True)
    try:
        archive = tarfile.open(path, "r:*")
    except tarfile.ReadError:
        if NOTICE.search(path.name):
            shutil.copyfile(path, target / path.name)
        return
    with archive:
        for item in archive:
            safe_path(item.name)
            if item.isfile() and PurePosixPath(item.name).suffix not in omit_formats \
                    and (NOTICE.search(PurePosixPath(item.name).name)
                                  or PurePosixPath(item.name).name in extra):
                dest = target / safe_path(item.name)
                dest.parent.mkdir(parents=True, exist_ok=True)
                with archive.extractfile(item) as source, dest.open("wb") as output:
                    shutil.copyfileobj(source, output)


def validate_link(path, root):
    safe_path(os.readlink(path))
    resolved = path.resolve(strict=True)
    if not resolved.is_relative_to(root.resolve()) or not resolved.is_file():
        raise ValueError("unsafe recipe symlink: " + str(path))


def file_inventory(root):
    result = {}
    for path in sorted(root.rglob("*")):
        name = str(path.relative_to(root))
        if name == "MANIFEST.json":
            continue
        if path.is_symlink():
            validate_link(path, root)
            result[name] = dict(link=os.readlink(path))
        elif path.is_dir():
            continue
        elif path.is_file():
            with path.open("rb") as stream:
                digest = hashlib.file_digest(stream, "sha256").hexdigest()
            result[name] = dict(sha256=digest, executable=bool(path.stat().st_mode & 0o111))
        else:
            raise ValueError("unsupported bundle file")
    return result


def pack(root, output):
    with output.open("wb") as raw, gzip.GzipFile(fileobj=raw, mode="wb", filename="", mtime=0) as gz:
        with tarfile.open(fileobj=gz, mode="w") as tar:
            for path in sorted(root.rglob("*")):
                if path.is_symlink():
                    validate_link(path, root)
                elif not (path.is_file() or path.is_dir()):
                    raise ValueError("unsafe bundle entry")
                info = tar.gettarinfo(str(path), str(path.relative_to(root)))
                info.uid = info.gid = 0
                info.uname = info.gname = ""
                info.mtime = 0
                info.mode = 0o755 if path.is_dir() or path.stat().st_mode & 0o111 else 0o644
                if path.is_file() and not path.is_symlink():
                    with path.open("rb") as stream:
                        tar.addfile(info, stream)
                else:
                    tar.addfile(info)


def collection_inputs(args):
    if bool(args.runtime) == bool(args.static_only):
        raise ValueError("choose runtime packages or static-only collection")
    runtime = packages(args.runtime.read_text()) if args.runtime else []
    linked = json.loads(args.linked.read_text())
    if args.static_only:
        validate_static_inputs(linked)
    return runtime, linked


def validate_static_inputs(linked):
    rows = linked.get("packages", [])
    if not rows or not linked.get("inputs"):
        raise ValueError("static-only collection requires linked runtime inputs")
    owners = set()
    for p in rows:
        # Reuse installed-package validation for externally supplied link metadata.
        fields = {"P": "name", "V": "version", "L": "licence", "o": "origin",
                  "c": "commit", "A": "architecture"}
        parsed = packages("\n".join(k + ":" + p[v] for k, v in fields.items()))
        if len(parsed) != 1 or public(parsed[0]) != {v: p[v] for v in fields.values()}:
            raise ValueError("invalid linked package metadata")
        if p["name"] in owners:
            raise ValueError("duplicate linked package owner")
        owners.add(p["name"])
    referenced = set()
    for item in linked["inputs"]:
        if "package" in item:
            if item["package"] not in owners or not item.get("path", "").startswith(("/usr/lib/", "/lib/")):
                raise ValueError("unrecognised linked system input")
            referenced.add(item["package"])
        elif item != dict(component="wasmvm", source="application source bundle"):
            raise ValueError("unrecognised application runtime input")
    if referenced != owners:
        raise ValueError("incomplete linked package ownership")


def collect(args):
    if os.getuid() == 0:
        raise ValueError("run source collection as an unprivileged user in the build stage")
    runtime, linked = collection_inputs(args)
    licence_data = [p for p in packages(Path("/lib/apk/db/installed").read_text())
                    if p["name"] == "spdx-licenses-text"]
    if len(licence_data) != 1:
        raise ValueError("install spdx-licenses-text in the collection stage")
    groups = group_sources({"runtime": runtime, "static-runtime": linked["packages"],
                            "licence-data": licence_data})
    args.output.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="os-source-") as temporary:
        work = Path(temporary)
        args.cache.mkdir(parents=True, exist_ok=True)
        repo = args.cache / "aports.git"
        if not repo.exists():
            run("git", "init", "--bare", "--quiet", str(repo))
            run("git", "-C", str(repo), "remote", "add", "origin", APORTS)
        run("git", "-C", str(repo), "config", "remote.origin.promisor", "true")
        run("git", "-C", str(repo), "config", "remote.origin.partialclonefilter", "blob:none")
        bundle = work / "bundle"
        bundle.mkdir()
        records = []
        for key, members in sorted(groups.items()):
            package = members[0]
            print("Collecting", package["origin"], package["version"], flush=True)
            dest = bundle / "sources" / key
            original = dest / "recipe"
            recipe_path = recipe(repo, package["commit"], package["origin"], original)
            # Preserve repository-wide notices governing recipe-only packages.
            roots = subprocess.check_output(["git", "-C", str(repo), "ls-tree", "-z", package["commit"]])
            for entry in roots.split(b"\x00"):
                if not entry:
                    continue
                info, name = entry.split(b"\t", 1)
                mode, kind, oid = info.decode().split()
                name = str(safe_path(name.decode()))
                if kind == "blob" and NOTICE.search(name):
                    p = original / "aports-notices" / name
                    p.parent.mkdir(parents=True, exist_ok=True)
                    p.write_bytes(subprocess.check_output(["git", "-C", str(repo), "cat-file", "blob", oid]))
            staging = work / "recipes" / key
            shutil.copytree(original, staging, symlinks=True)
            with (staging / "APKBUILD").open("a") as stream:
                stream.write(METADATA)
            meta = work / "metadata"
            env = dict(os.environ, ARK_SOURCE_METADATA=str(meta), SRCDEST=str(args.cache / "downloads" / key),
                       REPODEST=str(work / "packages"), SOURCE_DATE_EPOCH="0", DISTFILES_MIRROR=distfiles_mirror())
            subprocess.run(["abuild", "-m", "ark_source_metadata"], cwd=staging, env=env, check=True)
            metadata = meta.read_bytes().decode().split("\x00")[:-1]
            if len(metadata) != 5 or metadata[:2] != [package["origin"], package["version"]]:
                raise ValueError("recipe does not match installed package")
            sources = source_checks(metadata)
            subprocess.run(["abuild", "-m", "fetch", "verify"], cwd=staging, env=env, check=True)
            for record in sources:
                source = staging / "src" / record["file"]
                check_file(source, record)
                target = dest / "inputs" / record["file"]
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, target)
                extract_notices(target, bundle / "notices" / key,
                                EMBEDDED_NOTICES.get(package["origin"], ()),
                                # SPDX's complete machine-readable exports remain in inputs/;
                                # readable terms are copied and verified separately below.
                                (".nt", ".rdf", ".ttl", ".jsonld", ".json")
                                if package["origin"] == "spdx-licenses" else ())
            for file in original.rglob("*"):
                if file.is_file():
                    extract_notices(file, bundle / "notices" / key / "recipe" / file.parent.relative_to(original))
            records.append(dict(key=key, recipe=recipe_path, commit=package["commit"], packages=members,
                                inputs=sources, recipe_only=not sources))
        terms = bundle / "notices" / "licence-texts"
        terms.mkdir(parents=True)
        expressions = {p["licence"] for members in groups.values() for p in members}
        identifiers = set(re.findall(r"[A-Za-z0-9.+-]+", " ".join(expressions))) - {"AND", "OR", "WITH"}
        for identifier in sorted(identifiers):
            source = Path("/usr/share/spdx/text") / (identifier + ".txt")
            if not source.is_file():
                raise ValueError("missing standard licence text: " + identifier)
            shutil.copyfile(source, terms / source.name)
        # Retain each original recipe with its authorship/metadata alongside the texts.
        for record in records:
            target = bundle / "notices" / record["key"] / "recipe"
            target.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(bundle / "sources" / record["key"] / "recipe" / "APKBUILD", target / "APKBUILD")
        (bundle / "README.txt").write_text(
            "Alpine sources corresponding to the recorded installed packages and linked system runtimes.\n"
            "MANIFEST.json maps package versions to exact aports commits, recipes and checksum-verified inputs.\n"
            "Recipe-only packages are built entirely from the supplied recipe files.\n"
            "To build a package, use the matching Alpine architecture/toolchain with abuild. In recipe/,\n"
            "set SRCDEST to the absolute sibling inputs/ path and run abuild fetch verify, then abuild -r.\n"
            "For recipe-only packages create the empty inputs/ directory first. Build tools/dependencies\n"
            "are prerequisites; source completeness does not promise an offline or bit-identical OS build.\n"
            "notices/ is a convenience index; preserve all notices embedded in the complete sources too.\n"
            "licence-texts/ contains standard SPDX texts, including template placeholders where supplied;\n"
            "actual copyright holders and component terms are in the original sources and recipes.\n"
            "Operating-system programs retain their component licences independently of arkd.\n")
        manifest = dict(format=1, mode="static-only" if args.static_only else "runtime",
                        runtime_packages=[public(p) for p in runtime],
                        static_runtime=linked, licence_data=[public(p) for p in licence_data],
                        licence_texts=sorted(identifiers), sources=records, files=file_inventory(bundle))
        (bundle / "MANIFEST.json").write_text(json.dumps(manifest, indent=2) + "\n")
        verify_tree(bundle)
        archive = args.output / "corresponding-source.tar.gz"
        pack(bundle, archive)
        shutil.copyfile(bundle / "MANIFEST.json", args.output / "MANIFEST.json")
        shutil.copyfile(bundle / "README.txt", args.output / "README.txt")
        shutil.copytree(bundle / "notices", args.output / "notices", dirs_exist_ok=True)
        with archive.open("rb") as stream:
            checksum = hashlib.file_digest(stream, "sha256").hexdigest()
        (args.output / "corresponding-source.tar.gz.sha256").write_text(checksum + "  corresponding-source.tar.gz\n")


def verify_tree(root):
    manifest = json.loads((root / "MANIFEST.json").read_text())
    if manifest.get("mode") == "static-only":
        if manifest["runtime_packages"]:
            raise ValueError("static-only bundle contains unshipped runtime packages")
        validate_static_inputs(manifest["static_runtime"])
    expected = group_sources({"runtime": manifest["runtime_packages"],
                              "static-runtime": manifest["static_runtime"]["packages"],
                              "licence-data": manifest.get("licence_data", [])})
    actual = {r["key"]: r for r in manifest["sources"]}
    if len(actual) != len(manifest["sources"]) or set(actual) != set(expected):
        raise ValueError("incomplete source manifest")
    if manifest.get("format") != 1 or manifest.get("files") != file_inventory(root):
        raise ValueError("bundle file inventory mismatch")
    expressions = {p["licence"] for members in expected.values() for p in members}
    identifiers = set(re.findall(r"[A-Za-z0-9.+-]+", " ".join(expressions))) - {"AND", "OR", "WITH"}
    if set(manifest.get("licence_texts", [])) != identifiers:
        raise ValueError("incomplete licence text mapping")
    for identifier in identifiers:
        if not (root / "notices" / "licence-texts" / (identifier + ".txt")).is_file():
            raise ValueError("missing licence text")
    for key, members in expected.items():
        record = actual[key]
        if record["packages"] != members:
            raise ValueError("source package mapping mismatch")
        directory = root / "sources" / safe_path(key)
        if not (directory / "recipe" / "APKBUILD").is_file():
            raise ValueError("recipe is missing")
        if record["recipe_only"] != (not record["inputs"]):
            raise ValueError("invalid recipe-only record")
        for source in record["inputs"]:
            check_file(directory / "inputs" / safe_path(source["file"]), source)
        notices = root / "notices" / key
        origin = members[0]["origin"]
        if origin == "gcc" and not list(notices.rglob("COPYING.RUNTIME")):
            raise ValueError("missing GCC Runtime Library Exception")
        if origin in EMBEDDED_NOTICES:
            names = {p.name for p in notices.rglob("*") if p.is_file()}
            if not EMBEDDED_NOTICES[origin] <= names:
                raise ValueError("missing embedded source notices: " + origin)
    # Standard texts must be the actual data from the checksum-verified SPDX source.
    for package in manifest.get("licence_data", []):
        record = actual[source_key(package)]
        found = set()
        for source in record["inputs"]:
            path = root / "sources" / record["key"] / "inputs" / source["file"]
            try:
                archive = tarfile.open(path, "r:*")
            except tarfile.ReadError:
                continue
            with archive:
                for item in archive:
                    name = PurePosixPath(item.name)
                    identifier = name.stem
                    if item.isfile() and name.parent.name == "text" and identifier in identifiers:
                        expected_text = (root / "notices" / "licence-texts" / name.name).read_bytes()
                        if archive.extractfile(item).read() != expected_text:
                            raise ValueError("licence text differs from SPDX source: " + identifier)
                        found.add(identifier)
        if found != identifiers:
            raise ValueError("missing corresponding SPDX licence data")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    link = commands.add_parser("link-inputs")
    link.add_argument("--installed", type=Path, required=True)
    link.add_argument("--map", type=Path, required=True)
    link.add_argument("--output", type=Path, required=True)
    collect_parser = commands.add_parser("collect")
    mode = collect_parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--runtime", type=Path)
    mode.add_argument("--static-only", action="store_true")
    collect_parser.add_argument("--linked", type=Path, required=True)
    collect_parser.add_argument("--output", type=Path, required=True)
    collect_parser.add_argument("--cache", type=Path, default=Path("/tmp/ark-os-source-cache"))
    verify = commands.add_parser("verify")
    verify.add_argument("directory", type=Path)
    args = parser.parse_args()
    if args.command == "link-inputs":
        args.output.write_text(json.dumps(link_inputs(args.installed, args.map), indent=2) + "\n")
    elif args.command == "collect":
        collect(args)
    else:
        verify_tree(args.directory)


if __name__ == "__main__":
    main()
