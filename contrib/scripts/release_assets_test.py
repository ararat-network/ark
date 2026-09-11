#!/usr/bin/env python3
"""Tests for the release asset boundary, without building or contacting GitHub."""
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("release_assets", Path(__file__).with_name("release-assets.py"))
assets = importlib.util.module_from_spec(spec)
spec.loader.exec_module(assets)
REVISION = "a" * 40


class ReleaseAssetsTest(unittest.TestCase):
    def fixture(self, root, tag="v1.2.3"):
        dist, source = root / "dist", root / "source"
        dist.mkdir()
        source.mkdir()
        dist_names, source_names, checksum = assets.inventory(tag, REVISION)
        rows = []
        for folder, names in ((dist, dist_names), (source, source_names)):
            for name in sorted(names):
                path = folder / name
                path.write_bytes(name.encode())
                rows.append(f"{assets.digest(path)}  {name}\n")
        (dist / checksum).write_text("".join(sorted(rows)))
        return dist, source, checksum

    def test_complete_target_matrices(self):
        for tag, count in (("v1.2.3", 17), ("v1.2.3-rc.1", 17),
                           ("pricefeed/v1.2.3", 11), ("pricefeed/v1.2.3-rc.1", 11)):
            with self.subTest(tag=tag), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                dist, source, _ = self.fixture(root, tag)
                out = root / "release"
                assets.stage(tag, REVISION, dist, source, out)
                (out / assets.BUNDLE).write_text("bundle is verified separately by GitHub CLI")
                self.assertEqual(len(assets.verify(tag, REVISION, out)), count + 1)

    def test_invalid_identities(self):
        for tag, revision in (("main", REVISION), ("v01.2.3", REVISION),
                              ("v1.2.3-01", REVISION), ("pricefeed/v1.2", REVISION),
                              ("v1.2.3;touch bad", REVISION), ("v1.2.3", "a" * 7)):
            with self.subTest(tag=tag, revision=revision), self.assertRaises(ValueError):
                assets.identity(tag, revision)

    def test_bad_manifest_entries(self):
        for mutation in ("missing", "duplicate", "unexpected", "traversal"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                dist, source, checksum = self.fixture(root)
                path = dist / checksum
                lines = path.read_text().splitlines(True)
                if mutation == "missing":
                    lines.pop()
                elif mutation == "duplicate":
                    lines.append(lines[0])
                else:
                    name = "extra.txt" if mutation == "unexpected" else "../extra.txt"
                    lines.append("0" * 64 + "  " + name + "\n")
                path.write_text("".join(lines))
                with self.assertRaises(ValueError):
                    assets.stage("v1.2.3", REVISION, dist, source, root / "release")
                self.assertFalse((root / "release").exists())

    def test_tampered_missing_and_symlinked_inputs(self):
        for mutation in ("tampered", "missing", "symlink", "checksum-symlink"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                dist, source, checksum = self.fixture(root)
                victim = sorted(source.iterdir())[0]
                if mutation == "tampered":
                    victim.write_text("changed")
                elif mutation == "missing":
                    victim.unlink()
                else:
                    if mutation == "checksum-symlink":
                        victim = dist / checksum
                    other = root / "other"
                    other.write_bytes(victim.read_bytes())
                    victim.unlink()
                    victim.symlink_to(other)
                with self.assertRaises(ValueError):
                    assets.stage("v1.2.3", REVISION, dist, source, root / "release")

    def test_downloads_must_match_expected_assets(self):
        for mutation in ("missing", "extra", "tampered", "wrong-revision", "missing-bundle"):
            with self.subTest(mutation=mutation), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                dist, source, _ = self.fixture(root)
                out = root / "release"
                assets.stage("v1.2.3", REVISION, dist, source, out)
                (out / assets.BUNDLE).write_text("bundle")
                victim = next(out.glob("*.tar.gz"))
                revision = REVISION
                if mutation == "missing":
                    victim.unlink()
                elif mutation == "extra":
                    (out / "unreviewed.txt").write_text("extra")
                elif mutation == "tampered":
                    victim.write_text("changed")
                elif mutation == "wrong-revision":
                    revision = "b" * 40
                else:
                    (out / assets.BUNDLE).unlink()
                with self.assertRaises(ValueError):
                    assets.verify("v1.2.3", revision, out)

    def test_existing_output_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            dist, source, _ = self.fixture(root)
            out = root / "release"
            out.mkdir()
            (out / "keep").write_text("keep")
            with self.assertRaises(FileExistsError):
                assets.stage("v1.2.3", REVISION, dist, source, out)
            self.assertEqual((out / "keep").read_text(), "keep")


if __name__ == "__main__":
    unittest.main()
