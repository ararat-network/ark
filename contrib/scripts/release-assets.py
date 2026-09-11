#!/usr/bin/env python3
"""Stage and check the complete standalone release asset set; never publish."""
import argparse
import hashlib
from pathlib import Path
import re
import shutil

BUNDLE = "provenance.sigstore.json"
SEMVER = r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"


def identity(tag, revision):
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("expected a full source commit SHA")
    kind = "pricefeed" if tag.startswith("pricefeed/") else "arkd"
    prefix = "pricefeed/v" if kind == "pricefeed" else "v"
    if not tag.startswith(prefix) or not re.fullmatch(SEMVER, tag[len(prefix):]):
        raise ValueError("expected vX.Y.Z or pricefeed/vX.Y.Z, optionally with a prerelease suffix")
    version = tag[len(prefix):]
    if "-" in version:
        for part in version.split("-", 1)[1].split("."):
            if part.isdigit() and len(part) > 1 and part.startswith("0"):
                raise ValueError("numeric prerelease identifiers cannot have leading zeroes")
    return kind, version


def inventory(tag, revision):
    kind, version = identity(tag, revision)
    systems = ("linux", "darwin") if kind == "pricefeed" else ("linux",)
    archives = {f"{kind}-{version}-{system}-{arch}.tar.gz"
                for system in systems for arch in ("amd64", "arm64")}
    dist = archives | {name + ".spdx.json" for name in archives}
    stem = f"{kind}-source-{revision}"
    source = {stem + ending for ending in (".tar.gz", ".tar.gz.sha256", "-notices.tar.gz")}
    if kind == "arkd":
        for arch in ("amd64", "arm64"):
            stem = f"arkd-runtime-source-{revision}-linux-{arch}"
            source |= {stem + ending for ending in
                       (".tar.gz", ".tar.gz.sha256", "-notices.tar.gz", "-manifest.json", "-build.json")}
    checksum = f"SHA256SUMS-{'pricefeed-' if kind == 'pricefeed' else ''}{version}.txt"
    return dist, source, checksum


def digest(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"expected a regular file: {path.name}")
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def checksums(path, expected):
    digest(path)  # Reject symlinked manifests as well as symlinked assets.
    rows = {}
    for line in path.read_text().splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([A-Za-z0-9._+-]+)", line)
        if not match or match[2] in rows:
            raise ValueError("malformed or duplicate checksum entry")
        rows[match[2]] = match[1]
    if set(rows) != expected:
        raise ValueError("checksum inventory differs from the required release assets")
    return rows


def stage(tag, revision, dist, source, output):
    dist_names, source_names, checksum = inventory(tag, revision)
    rows = checksums(dist / checksum, dist_names | source_names)
    inputs = {name: dist / name for name in dist_names}
    inputs.update({name: source / name for name in source_names})
    inputs[checksum] = dist / checksum
    # Validate every input before creating the output directory.
    for name, path in inputs.items():
        actual = digest(path)
        if name in rows and actual != rows[name]:
            raise ValueError(f"checksum mismatch: {name}")
        if path.stat().st_size >= 2**31:
            raise ValueError(f"asset exceeds the GitHub release size limit: {name}")
    output.mkdir(parents=True, exist_ok=False)
    for name, path in sorted(inputs.items()):
        shutil.copyfile(path, output / name)
    verify(tag, revision, output, signed=False)


def verify(tag, revision, directory, signed=True):
    dist_names, source_names, checksum = inventory(tag, revision)
    expected = dist_names | source_names
    files = expected | {checksum} | ({BUNDLE} if signed else set())
    if {p.name for p in directory.iterdir()} != files:
        raise ValueError("release directory has missing or unexpected assets")
    rows = checksums(directory / checksum, expected)
    for name, want in rows.items():
        if digest(directory / name) != want:
            raise ValueError(f"checksum mismatch: {name}")
    if signed:
        digest(directory / BUNDLE)  # Cryptographic verification belongs to gh attestation verify.
    return sorted(expected | {checksum})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=("identity", "stage", "verify"))
    parser.add_argument("tag")
    parser.add_argument("revision")
    parser.add_argument("--dist", type=Path, default=Path("dist"))
    parser.add_argument("--source", type=Path, default=Path("build/source"))
    parser.add_argument("--directory", type=Path)
    args = parser.parse_args()
    if args.operation == "identity":
        print(identity(args.tag, args.revision)[0])
    elif args.directory is None:
        parser.error("--directory is required")
    elif args.operation == "stage":
        stage(args.tag, args.revision, args.dist, args.source, args.directory)
    else:
        for name in verify(args.tag, args.revision, args.directory):
            print(name)


if __name__ == "__main__":
    main()
