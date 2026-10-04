#!/usr/bin/env python3
"""Focused source-coverage tests; no network or abuild execution."""
import importlib.util
import hashlib
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

spec = importlib.util.spec_from_file_location("os_source", Path(__file__).with_name("package-os-source.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class SourceCoverageTests(unittest.TestCase):
    def test_collection_modes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            linked = root / "linked.json"
            value = dict(packages=[self.package()], inputs=[dict(path="/usr/lib/libc.a", package="busybox")])
            linked.write_text(json.dumps(value))
            options = SimpleNamespace(runtime=None, static_only=True, linked=linked)
            self.assertEqual(m.collection_inputs(options), ([], value))
            for runtime, static_only in ((None, False), (root / "runtime", True)):
                with self.subTest(runtime=runtime, static_only=static_only), self.assertRaises(ValueError):
                    m.collection_inputs(SimpleNamespace(runtime=runtime, static_only=static_only, linked=linked))
            for bad in (dict(packages=[], inputs=[]), dict(value, inputs=[]),
                        dict(value, inputs=[dict(path="/usr/lib/libc.a", package="unknown")]),
                        dict(value, inputs=[dict(component="unknown")])):
                with self.subTest(bad=bad), self.assertRaises(ValueError):
                    m.validate_static_inputs(bad)
            empty = root / "runtime"
            empty.write_text("")
            with self.assertRaises(ValueError):
                m.collection_inputs(SimpleNamespace(runtime=empty, static_only=False, linked=linked))

    def package(self, name="busybox", version="1.0-r0"):
        return dict(name=name, version=version, licence="GPL-2.0-only", origin="busybox",
                    commit="a" * 40, architecture="x86_64")

    def test_parse_inventory(self):
        row = "P:busybox\nV:1.0-r0\nL:GPL-2.0-only\no:busybox\nc:" + "a" * 40 + "\nA:x86_64\nF:bin\nR:busybox\n"
        with self.subTest("valid"):
            self.assertEqual(m.packages(row)[0]["files"], ["/bin/busybox"])
        for bad in (row.replace("L:GPL-2.0-only\n", ""), row + "P:again\n", row.replace("a" * 40, "HEAD")):
            with self.subTest(bad=bad):
                with self.assertRaises(ValueError):
                    m.packages(bad)

    def test_origins_keep_distinct_versions(self):
        inputs = {"runtime": [self.package(), self.package("busybox-binsh")],
                  "static-runtime": [self.package(version="2.0-r0")]}
        self.assertEqual(len(m.group_sources(inputs)), 2)
        inputs["runtime"].append(self.package(version="3.0-r0"))
        with self.assertRaises(ValueError):
            m.group_sources(inputs)

    def test_paths(self):
        for path in ("../escape", "/absolute", "a/../../escape", ""):
            with self.subTest(path=path), self.assertRaises(ValueError):
                m.safe_path(path)
        self.assertEqual(str(m.safe_path("patches/fix.patch")), "patches/fix.patch")

    def test_checked_sources_and_recipe_only(self):
        self.assertEqual(m.source_checks(["base", "1-r0", "", "", ""]), [])
        digest = hashlib.sha512(b"source").hexdigest()
        rows = m.source_checks(["p", "1-r0", "s.tar::https://example.test/v1.tar", digest + "  s.tar", ""])
        with tempfile.TemporaryDirectory() as tmp:
            p = Path(tmp) / "s.tar"
            p.write_bytes(b"source")
            m.check_file(p, rows[0])
            p.write_bytes(b"corrupt")
            with self.assertRaises(ValueError):
                m.check_file(p, rows[0])
        with self.assertRaises(ValueError):
            m.source_checks(["p", "1-r0", "https://example.test/missing.tar", "", ""])

    def test_missing_mapping_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "MANIFEST.json").write_text(json.dumps(dict(runtime_packages=[self.package()],
                static_runtime=dict(packages=[]), sources=[])))
            with self.assertRaises(ValueError):
                m.verify_tree(root)

    def test_link_ownership_and_unknown_inputs(self):
        with tempfile.TemporaryDirectory() as tmp:
            db, link = Path(tmp) / "installed", Path(tmp) / "map"
            db.write_text("P:musl-dev\nV:1-r0\nL:MIT\no:musl\nc:" + "a" * 40
                          + "\nA:x86_64\nF:usr/lib\nR:libc.a\n")
            known = ("LOAD /usr/lib/libc.a\nLOAD /tmp/go-link-12/000001.o\n"
                     "LOAD /lib/libwasmvm_muslc.x86_64.a\n")
            with patch.object(m.os.path, "realpath", side_effect=lambda p: p):
                link.write_text(known)
                result = m.link_inputs(db, link)
                self.assertEqual([p["name"] for p in result["packages"]], ["musl-dev"])
                self.assertEqual(len(result["inputs"]), 2)
                link.write_text(known + "LOAD linker stubs\n")
                with self.assertRaises(ValueError):
                    m.link_inputs(db, link)
                db.write_text(db.read_text().replace("A:x86_64", "A:aarch64"))
                stub_result = m.link_inputs(db, link)
                self.assertEqual(stub_result["inputs"], result["inputs"])
                self.assertEqual(stub_result["packages"][0]["architecture"], "aarch64")
                for data in ("", known + "LOAD /unknown/library.a\n", "LOAD relative.a\n"):
                    with self.subTest(data=data):
                        link.write_text(data)
                        with self.assertRaises(ValueError):
                            m.link_inputs(db, link)

    def test_recipe_symlinks(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "original").write_text("install script")
            link = root / "alias"
            link.symlink_to("original")
            m.validate_link(link, root)
            for target in ("../outside", "/etc/passwd", "missing", "alias"):
                with self.subTest(target=target):
                    link.unlink()
                    link.symlink_to(target)
                    with self.assertRaises((ValueError, OSError, RuntimeError)):
                        m.validate_link(link, root)

    def test_archive_notice_paths(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            archive = root / "licence-source.tar"
            for name, expected in (("pkg/LICENSE", True), ("pkg/certdata.txt", True),
                                   ("pkg/unrelated", False), ("pkg/licenses.json", False),
                                   ("../LICENSE", None)):
                with self.subTest(name=name):
                    with tarfile.open(archive, "w") as tar:
                        member = tarfile.TarInfo(name)
                        member.size = 7
                        tar.addfile(member, io.BytesIO(b"licence"))
                    if expected is None:
                        with self.assertRaises(ValueError):
                            m.extract_notices(archive, root / "notices")
                    else:
                        m.extract_notices(archive, root / "notices", {"certdata.txt"}, (".json",))
                        self.assertEqual((root / "notices" / name).exists(), expected)
                        self.assertFalse((root / "notices" / archive.name).exists())

    def test_offline_recipe_input_and_licence_integrity(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            p = self.package()
            key = m.source_key(p)
            recipe = root / "sources" / key / "recipe" / "APKBUILD"
            recipe.parent.mkdir(parents=True)
            recipe.write_text("recipe and build configuration")
            term = root / "notices" / "licence-texts" / "GPL-2.0-only.txt"
            term.parent.mkdir(parents=True)
            term.write_text("licence fixture")
            manifest = dict(format=1, runtime_packages=[p], static_runtime=dict(packages=[]),
                            licence_texts=["GPL-2.0-only"], sources=[dict(key=key,
                            packages=[dict(p, scope="runtime")], inputs=[], recipe_only=True)],
                            files=m.file_inventory(root))
            (root / "MANIFEST.json").write_text(json.dumps(manifest))
            m.verify_tree(root)
            recipe.write_text("changed")
            with self.assertRaises(ValueError):
                m.verify_tree(root)
            manifest["files"] = m.file_inventory(root)
            term.unlink()
            manifest["files"] = m.file_inventory(root)
            (root / "MANIFEST.json").write_text(json.dumps(manifest))
            with self.assertRaises(ValueError):
                m.verify_tree(root)

    def test_deterministic_archive(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "source"
            root.mkdir()
            p = root / "script"
            p.write_text("source")
            p.chmod(0o755)
            (root / "alias").symlink_to("script")
            a, b = Path(tmp) / "a.gz", Path(tmp) / "b.gz"
            m.pack(root, a)
            os.utime(p, (1234, 1234))
            m.pack(root, b)
            self.assertEqual(a.read_bytes(), b.read_bytes())
            with tarfile.open(a) as tar:
                self.assertEqual(tar.getmember("script").uid, 0)
                self.assertEqual(tar.getmember("script").uname, "")
                self.assertTrue(tar.getmember("alias").issym())


if __name__ == "__main__":
    unittest.main()
