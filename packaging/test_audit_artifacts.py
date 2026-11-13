from __future__ import annotations

import hashlib
import tempfile
import unittest
from pathlib import Path

import audit_artifacts


class FinalBundleAuditTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary_directory.name)
        self.packages = ["first.bin", "second.bin"]
        for index, name in enumerate(self.packages, start=1):
            (self.root / name).write_bytes(bytes([index]) * 128)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def write_manifest(self, names: list[str] | None = None) -> None:
        lines = []
        for name in names or self.packages:
            digest = hashlib.sha256((self.root / name).read_bytes()).hexdigest()
            lines.append(f"{digest}  {name}\n")
        (self.root / "SHA256SUMS").write_text("".join(lines), encoding="ascii")

    def test_combined_manifest_accepts_exact_package_set(self) -> None:
        self.write_manifest()
        audit_artifacts.check_combined_manifest(self.root, self.packages)

    def test_combined_manifest_rejects_duplicate_target(self) -> None:
        self.write_manifest(["first.bin", "first.bin"])
        with self.assertRaisesRegex(SystemExit, "duplicate target"):
            audit_artifacts.check_combined_manifest(self.root, self.packages)

    def test_combined_manifest_rejects_checksum_mismatch(self) -> None:
        self.write_manifest()
        (self.root / "first.bin").write_bytes(b"changed" * 32)
        with self.assertRaisesRegex(SystemExit, "combined checksum mismatch"):
            audit_artifacts.check_combined_manifest(self.root, self.packages)

    def test_combined_manifest_rejects_path_target(self) -> None:
        digest = hashlib.sha256((self.root / "first.bin").read_bytes()).hexdigest()
        (self.root / "SHA256SUMS").write_text(
            f"{digest}  ..\\first.bin\n",
            encoding="ascii",
        )
        with self.assertRaisesRegex(SystemExit, "invalid or duplicate target"):
            audit_artifacts.check_combined_manifest(self.root, self.packages)

    def test_sidecar_rejects_non_portable_crlf(self) -> None:
        name = "first.bin"
        digest = hashlib.sha256((self.root / name).read_bytes()).hexdigest()
        (self.root / f"{name}.sha256").write_bytes(
            f"{digest}  {name}\r\n".encode("ascii")
        )
        with self.assertRaisesRegex(SystemExit, "portable LF endings"):
            audit_artifacts.check_sidecar(self.root, name)

    def test_build_provenance_requires_exact_values(self) -> None:
        commit = "a" * 40
        run = "12345"
        (self.root / "RC_BUILD.txt").write_text(
            f"source_commit={commit}\nworkflow_run={run}\n",
            encoding="ascii",
        )
        audit_artifacts.check_build_provenance(self.root, commit, run)
        with self.assertRaisesRegex(SystemExit, "provenance mismatch"):
            audit_artifacts.check_build_provenance(self.root, "b" * 40, run)


if __name__ == "__main__":
    unittest.main()
