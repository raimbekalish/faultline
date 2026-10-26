#!/usr/bin/env python3
"""Unit checks for packaging without compiling binaries or publishing anything."""
import importlib.util
from pathlib import Path
import tarfile
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("packager", Path(__file__).with_name("package-release.py"))
packager = importlib.util.module_from_spec(spec)
spec.loader.exec_module(packager)


class Packaging(unittest.TestCase):
    def test_archive_determinism_and_contents(self):
        with tempfile.TemporaryDirectory() as d:
            d = Path(d)
            files = {"bin": (b"synthetic executable fixture", 0o755), "LICENSE": (b"fixture", 0o644)}
            packager.archive(d / "a.tar.gz", files, 123456)
            packager.archive(d / "b.tar.gz", files, 123456)
            self.assertEqual((d / "a.tar.gz").read_bytes(), (d / "b.tar.gz").read_bytes())
            with tarfile.open(d / "a.tar.gz") as t:
                self.assertEqual(t.getnames(), ["LICENSE", "bin"])
                self.assertTrue(all(m.isfile() and m.uid == 0 and m.mtime == 123456 for m in t))
                self.assertEqual(t.getmember("bin").mode, 0o755)
            with self.assertRaises(FileExistsError):
                packager.archive(d / "a.tar.gz", files, 123456)
            with self.assertRaises(ValueError):
                packager.archive(d / "bad.tar.gz", {"../bad": (b"", 0o644)}, 0)

    def test_publish_never_replaces_and_failed_cleanup(self):
        with tempfile.TemporaryDirectory() as d:
            d = Path(d)
            stage = d / "stage"
            stage.mkdir()
            (stage / "a").write_bytes(b"archive")
            out = d / "out"
            packager.publish(stage, out)
            with self.assertRaises(FileExistsError):
                packager.publish(stage, out)
            self.assertEqual((out / "a").read_bytes(), b"archive")
            # A non-file source causes link failure after a prior publication.
            (stage / "z-directory").mkdir()
            with self.assertRaises(OSError):
                packager.publish(stage, d / "failed")
            self.assertFalse((d / "failed").exists())


if __name__ == "__main__":
    unittest.main()
