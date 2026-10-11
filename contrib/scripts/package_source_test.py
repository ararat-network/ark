"""Source packaging's path-safety and privacy boundaries (stdlib-only)."""

import importlib.util
from pathlib import Path
import tarfile
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location("package_source", Path(__file__).with_name("package-source.py"))
source = importlib.util.module_from_spec(spec)
spec.loader.exec_module(source)


class SourceArchiveTests(unittest.TestCase):
    def test_stable_archive_omits_host_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tree = root / "source"
            tree.mkdir()
            script = tree / "build.sh"
            script.write_text("echo build\n")
            script.chmod(0o755)
            for name in ("one.tgz", "two.tgz"):
                source.pack(tree, root / name, 123)
            self.assertEqual((root / "one.tgz").read_bytes(), (root / "two.tgz").read_bytes())
            with tarfile.open(root / "one.tgz") as archive:
                member = archive.getmember("build.sh")
                self.assertEqual((member.uid, member.gid, member.uname, member.gname, member.mtime), (0, 0, "", "", 123))
                self.assertEqual(member.mode, 0o755)
                self.assertEqual(archive.extractfile(member).read(), b"echo build\n")

    def test_source_symlink_cannot_include_files_outside_export(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            tree = root / "source"
            tree.mkdir()
            (root / "private").write_text("must not be included")
            (tree / "escape").symlink_to(root / "private")
            with self.assertRaises(ValueError):
                source.pack(tree, root / "output.tgz", 123)

    def test_module_archive_paths(self):
        for relative in ("src/library.c", "../../private", "/absolute"):
            with self.subTest(path=relative), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                archive_path = root / "module.zip"
                with zipfile.ZipFile(archive_path, "w") as archive:
                    archive.writestr("example.org/Module/v3@v3.1.0/" + relative, "source")
                if relative == "src/library.c":
                    source.extract_module(archive_path, root / "out")
                    self.assertEqual((root / "out/src/library.c").read_text(), "source")
                else:
                    with self.assertRaises(ValueError):
                        source.extract_module(archive_path, root / "out")

    def test_local_replacement_rejected(self):
        with self.assertRaisesRegex(ValueError, "local module replacements"):
            source.effective({"Path": "example.org/lib", "Replace": {"Path": "../private"}})

    def test_busl_must_be_the_locked_crates_own(self):
        lock = '[[package]]\nname = "wasmer-compiler-singlepass"\nversion = "7.4.2"\n'
        for name, lock_text, licence, error in (
            ("matching", lock, b"terms", None),
            ("changed terms", lock, b"new terms", "differs"),
            ("crate gone", '[[package]]\nname = "other"\nversion = "1.0.0"\n', b"terms", "no longer locked"),
        ):
            with self.subTest(name), tempfile.TemporaryDirectory() as tmp:
                manifest = Path(tmp)
                (manifest / "Cargo.lock").write_text(lock_text)
                vendored = manifest / "vendor/wasmer-compiler-singlepass-7.4.2/LICENSE"
                vendored.parent.mkdir(parents=True)
                vendored.write_bytes(licence)
                if error is None:
                    source.check_busl(manifest, b"terms")
                else:
                    with self.assertRaisesRegex(ValueError, error):
                        source.check_busl(manifest, b"terms")

    def test_module_proxy_case_and_major_version(self):
        self.assertEqual(str(source.proxy_path({"Path": "github.com/CosmWasm/wasmvm/v3", "Version": "v3.0.7"})),
                         "github.com/!cosm!wasm/wasmvm/v3/@v/v3.0.7")


if __name__ == "__main__":
    unittest.main()
