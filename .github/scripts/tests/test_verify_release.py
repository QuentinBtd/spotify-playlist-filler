"""Offline regression tests of the existing release verifier, with local fixtures."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("verifier", Path(__file__).resolve().parents[1] / "verify-release.py")
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class VerifyTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.dist = Path(self.temporary.name)
        (self.dist / "metadata.json").write_text(json.dumps({"version": "1.2.0"}))
        self.names = [f"spotify-playlist-filler_1.2.0_{os}_{arch}.{ext}"
                      for os, ext in [("linux", "tar.gz"), ("darwin", "tar.gz"), ("windows", "zip")]
                      for arch in ["amd64", "arm64"]]
        # Deliberately synthetic bytes: this tests integrity/names, not archive format.
        for name in self.names:
            (self.dist / name).write_bytes(b"offline-checksum-fixture")
        self.lines = [f"{hashlib.sha256((self.dist / name).read_bytes()).hexdigest()}  {name}"
                      for name in self.names]
        (self.dist / "checksums.txt").write_text("\n".join(self.lines))
        self.release = self.dist / "release.json"
        self.index = self.dist / "index.json"
        self.release.write_text(json.dumps({"isDraft": True, "tagName": "v1.2.0",
                                           "assets": [{"name": n} for n in self.names + ["checksums.txt"]]}))
        self.index.write_text(json.dumps({"manifests": [{"platform": {"os": "linux", "architecture": arch}}
                                                       for arch in ["amd64", "arm64"]]}))

    def test_valid_local_checksums_draft_and_index(self):
        verifier.verify(self.dist, self.release, self.index, "v1.2.0")

    def test_corruption_blocks(self):
        (self.dist / self.names[0]).write_bytes(b"corrupted")
        with self.assertRaisesRegex(ValueError, "Checksum mismatch"):
            verifier.verify(self.dist)

    def test_duplicate_checksum_blocks(self):
        (self.dist / "checksums.txt").write_text("\n".join(self.lines + [self.lines[0]]))
        with self.assertRaisesRegex(ValueError, "duplicate"):
            verifier.verify(self.dist)

    def test_path_traversal_blocks(self):
        (self.dist / "checksums.txt").write_text("0" * 64 + "  ../outside")
        with self.assertRaisesRegex(ValueError, "Invalid"):
            verifier.verify(self.dist)

    def test_wrong_tag_blocks(self):
        with self.assertRaisesRegex(ValueError, "matching draft"):
            verifier.verify(self.dist, self.release, tag="v9.9.9")

    def test_nondraft_blocks(self):
        data = json.loads(self.release.read_text())
        data["isDraft"] = False
        self.release.write_text(json.dumps(data))
        with self.assertRaisesRegex(ValueError, "matching draft"):
            verifier.verify(self.dist, self.release, tag="v1.2.0")

    def test_missing_asset_blocks(self):
        data = json.loads(self.release.read_text())
        data["assets"].pop()
        self.release.write_text(json.dumps(data))
        with self.assertRaisesRegex(ValueError, "Missing uploaded"):
            verifier.verify(self.dist, self.release, tag="v1.2.0")

    def test_wrong_platform_blocks(self):
        self.index.write_text(json.dumps({"manifests": [{"platform": {"os": "linux", "architecture": "amd64"}}]}))
        with self.assertRaisesRegex(ValueError, "Unexpected image platforms"):
            verifier.verify(self.dist, image_index=self.index)


if __name__ == "__main__":
    unittest.main()
