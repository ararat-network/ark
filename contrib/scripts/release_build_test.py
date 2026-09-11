#!/usr/bin/env python3
"""Release adapter boundary tests, using small fixtures and no Docker/network."""
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import struct
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("release_build", Path(__file__).with_name("release-build.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class ReleaseBuildTests(unittest.TestCase):
    def arguments(self, root):
        return ["build", "-trimpath", "-ldflags", '-X example.Version=v1 -extldflags "-Wl,-Map,' + m.MAP
                + ' -static"', "-o", str(root / "dist/with spaces/arkd"), "./cmd/arkd"]

    def test_arguments_and_targets(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for arch, cpu_key, cpu in (("amd64", "GOAMD64", "v2"), ("arm64", "GOARM64", "v8.1")):
                with self.subTest(arch=arch):
                    env = dict(GOOS="linux", GOARCH=arch, CGO_ENABLED="1", **{cpu_key: cpu})
                    args = self.arguments(root)
                    request, output = m.build_request(args, env, root)
                    self.assertEqual(output, (root / "dist/with spaces/arkd").resolve())
                    self.assertEqual(request["args"][3], args[3])
                    self.assertEqual(request["args"][-2], m.BINARY)
                    self.assertEqual(request["env"][cpu_key], cpu)
                    self.assertNotIn(str(root), json.dumps(request))
            for env in (dict(GOOS="darwin", GOARCH="arm64", CGO_ENABLED="1"),
                        dict(GOOS="linux", GOARCH="386", CGO_ENABLED="1"),
                        dict(GOOS="linux", GOARCH="amd64", CGO_ENABLED="0"),
                        dict(GOOS="linux", GOARCH="amd64", CGO_ENABLED="1", GOAMD64="../escape")):
                with self.subTest(env=env), self.assertRaises(ValueError):
                    m.build_request(self.arguments(root), env, root)

    def test_reject_bad_command_and_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            env = dict(GOOS="linux", GOARCH="amd64", CGO_ENABLED="1")
            cases = []
            for index, value in ((0, "test"), (-1, "./other"), (-2, str(root / "outside/arkd")),
                                 (-2, str(root / "dist/file")), (3, "-static")):
                args = self.arguments(root)
                args[index] = value
                cases.append(args)
            cases.append(self.arguments(root) + ["-o", "another"])
            (root / "dist").mkdir()
            (root / "dist/escape").symlink_to(root, target_is_directory=True)
            args = self.arguments(root);args[-2] = str(root / "dist/escape/arkd");cases.append(args)
            for args in cases:
                with self.subTest(args=args), self.assertRaises(ValueError):
                    m.build_request(args, env, root)

    def fixture_bundle(self, root, revision="a" * 40, kind="arkd", name="ark/go.mod"):
        archive = root / "source.tar.gz"
        with tarfile.open(archive, "w:gz") as tar:
            for path, data in (("SOURCE-MANIFEST.json", json.dumps(dict(kind=kind, revision=revision)).encode()),
                               (name, b"source fixture")):
                item = tarfile.TarInfo(path);item.size = len(data)
                tar.addfile(item, io.BytesIO(data))
        Path(str(archive) + ".sha256").write_text(m.digest(archive) + "  source.tar.gz\n")
        return archive

    def test_source_checksum_revision_and_paths(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            archive = self.fixture_bundle(root)
            m.unpack_source(archive, "a" * 40, root / "valid")
            self.assertEqual((root / "valid/go.mod").read_bytes(), b"source fixture")
            with self.assertRaises(ValueError):
                m.unpack_source(archive, "b" * 40, root / "wrong-revision")
            archive.write_bytes(archive.read_bytes() + b"corrupt")
            with self.assertRaises(ValueError):
                m.unpack_source(archive, "a" * 40, root / "corrupt")
            for name in ("ark/../outside", "/outside", "ark/.git/config", "ark/" + m.REQUEST):
                with self.subTest(name=name):
                    archive = self.fixture_bundle(root, name=name)
                    with self.assertRaises(ValueError):
                        m.unpack_source(archive, "a" * 40, root / "unsafe")
            archive = self.fixture_bundle(root, kind="image")
            with self.assertRaises(ValueError):
                m.unpack_source(archive, "a" * 40, root / "wrong-kind")

    def elf(self, machine=62, program_type=1):
        header = bytearray(64)
        header[:6] = b"\x7fELF\x02\x01"
        struct.pack_into("<H", header, 18, machine)
        struct.pack_into("<Q", header, 32, 64)
        struct.pack_into("<HH", header, 54, 56, 1)
        program = bytearray(56);struct.pack_into("<I", program, 0, program_type)
        return header + program

    def test_binary_architecture_and_static_linkage(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "arkd"
            for arch, machine in (("amd64", 62), ("arm64", 183)):
                with self.subTest(arch=arch):
                    path.write_bytes(self.elf(machine))
                    m.check_binary(path, arch)
            for data in (self.elf(183), self.elf(program_type=2), self.elf(program_type=3), b"bad", self.elf()[:90]):
                with self.subTest(data=data[:20]):
                    path.write_bytes(data)
                    with self.assertRaises(ValueError):
                        m.check_binary(path, "amd64")

    def test_stale_export_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            request = dict(env=dict(GOARCH="amd64"), revision="a" * 40, args=["build"], source_sha256="s")
            binary = root / "arkd";binary.write_bytes(self.elf())
            valid = dict(revision=request["revision"], target=request["env"], build_args=request["args"],
                         application_source_sha256="s", binary_sha256=m.digest(binary))
            for key in valid:
                with self.subTest(key=key):
                    record = dict(valid);record[key] = "stale"
                    (root / "BUILD.json").write_text(json.dumps(record))
                    with self.assertRaisesRegex(ValueError, "identity mismatch"):
                        m.verify_export(root, request, None)


if __name__ == "__main__":
    unittest.main()
