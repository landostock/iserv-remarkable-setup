#!/usr/bin/env python3
"""Fail-closed audit for the merged release-candidate artifact directory."""

from __future__ import annotations

import argparse
import hashlib
import re
import tarfile
from pathlib import Path, PurePosixPath

ROOT = Path(__file__).resolve().parents[1]
VERSION_TEXT = (ROOT / "cmd/setup/version.go").read_text(encoding="utf-8")
VERSION_MATCH = re.search(r'^var appVersion = "([^"]+)"$', VERSION_TEXT, re.MULTILINE)
if not VERSION_MATCH:
    raise SystemExit("appVersion not found while auditing artifacts")
EXPECTED_VERSION = VERSION_MATCH.group(1)
FINAL_BUNDLE_FILES = {
    "PRIVACY.md",
    "RC_BUILD.txt",
    "RELEASE_NOTES_RC.md",
    "SECURITY.md",
    "SHA256SUMS",
    "THIRD_PARTY_NOTICES.md",
}


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def require_file(root: Path, name: str, minimum: int = 1) -> Path:
    path = root / name
    if not path.is_file() or path.stat().st_size < minimum:
        raise SystemExit(f"missing or unexpectedly small artifact: {name}")
    return path


def is_plain_filename(name: str) -> bool:
    return bool(name) and "/" not in name and "\\" not in name and Path(name).name == name


def check_sidecar(root: Path, name: str) -> None:
    target = require_file(root, name, 128)
    sidecar = require_file(root, name + ".sha256")
    raw = sidecar.read_bytes()
    if b"\r" in raw:
        raise SystemExit(f"checksum sidecar must use portable LF endings: {sidecar.name}")
    line = raw.decode("ascii").strip()
    match = re.fullmatch(r"([0-9a-fA-F]{64})\s+\*?(.+)", line)
    if not match:
        raise SystemExit(f"invalid checksum sidecar: {sidecar.name}")
    if not is_plain_filename(match.group(2)) or match.group(2) != target.name:
        raise SystemExit(f"checksum sidecar names wrong target: {sidecar.name}")
    if match.group(1).lower() != sha256(target):
        raise SystemExit(f"checksum mismatch: {target.name}")


def check_combined_manifest(root: Path, packages: list[str]) -> None:
    manifest = require_file(root, "SHA256SUMS")
    entries: dict[str, str] = {}
    for line in manifest.read_text(encoding="ascii").splitlines():
        match = re.fullmatch(r"([0-9a-fA-F]{64})\s+\*?(.+)", line)
        if not match:
            raise SystemExit("invalid line in SHA256SUMS")
        name = match.group(2)
        if not is_plain_filename(name) or name in entries:
            raise SystemExit(f"invalid or duplicate target in SHA256SUMS: {name}")
        entries[name] = match.group(1).lower()

    if set(entries) != set(packages):
        raise SystemExit(
            "unexpected SHA256SUMS target set: "
            f"missing={sorted(set(packages) - set(entries))} "
            f"unexpected={sorted(set(entries) - set(packages))}"
        )
    for name in packages:
        if entries[name] != sha256(require_file(root, name, 128)):
            raise SystemExit(f"combined checksum mismatch: {name}")


def check_build_provenance(root: Path, expected_commit: str, expected_run: str) -> None:
    if not re.fullmatch(r"[0-9a-f]{40}", expected_commit):
        raise SystemExit("expected final commit must be a lowercase 40-character Git SHA")
    if not re.fullmatch(r"[1-9][0-9]*", expected_run):
        raise SystemExit("expected final workflow run must be a positive integer")

    lines = require_file(root, "RC_BUILD.txt").read_text(encoding="ascii").splitlines()
    expected_lines = [
        f"source_commit={expected_commit}",
        f"workflow_run={expected_run}",
    ]
    if lines != expected_lines:
        raise SystemExit(
            f"RC_BUILD.txt provenance mismatch: {lines!r} != {expected_lines!r}"
        )


def safe_tar_members(path: Path) -> set[str]:
    names: set[str] = set()
    with tarfile.open(path, "r:gz") as archive:
        for member in archive.getmembers():
            pure = PurePosixPath(member.name)
            if pure.is_absolute() or ".." in pure.parts or "\\" in member.name:
                raise SystemExit(f"unsafe archive member in {path.name}: {member.name}")
            if member.issym() or member.islnk():
                target = PurePosixPath(member.linkname)
                if target.is_absolute() or ".." in target.parts or "\\" in member.linkname:
                    raise SystemExit(f"unsafe archive link in {path.name}: {member.name} -> {member.linkname}")
            names.add(member.name)
    return names


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Fail-closed audit for release-candidate artifacts"
    )
    parser.add_argument("artifact_dir", type=Path)
    parser.add_argument("--final-commit")
    parser.add_argument("--final-run")
    args = parser.parse_args()
    final_values_supplied = (
        args.final_commit is not None,
        args.final_run is not None,
    )
    if any(final_values_supplied) and not all(final_values_supplied):
        parser.error("--final-commit and --final-run must be supplied together")
    final_bundle = all(final_values_supplied)

    root = args.artifact_dir.resolve()
    if not root.is_dir():
        raise SystemExit("artifact directory does not exist")

    packages = [
        "Corresponding-Source-remarquee-rmapi.tar.gz",
        "IServ-reMarkable-linux-amd64.tar.gz",
        "IServ-reMarkable-linux-amd64.deb",
        "IServ-reMarkable-linux-arm64.tar.gz",
        "IServ-reMarkable-linux-arm64.deb",
        "IServ-reMarkable-windows-amd64-Setup.exe",
        "IServ-reMarkable-windows-arm64-Setup.exe",
        "IServ-reMarkable-macos.dmg",
    ]
    version_files = [
        "IServ-reMarkable-linux-amd64.version.txt",
        "IServ-reMarkable-linux-arm64.version.txt",
        "IServ-reMarkable-windows.version.txt",
        "IServ-reMarkable-macos.version.txt",
    ]
    expected_files = set(packages) | {name + ".sha256" for name in packages} | set(version_files)
    if final_bundle:
        expected_files |= FINAL_BUNDLE_FILES
    actual_files = {path.name for path in root.iterdir() if path.is_file()}
    unexpected = sorted(actual_files - expected_files)
    missing = sorted(expected_files - actual_files)
    if unexpected or missing:
        raise SystemExit(f"unexpected RC artifact set: missing={missing} unexpected={unexpected}")

    for package in packages:
        check_sidecar(root, package)

    for name in version_files:
        value = require_file(root, name).read_text(encoding="ascii").strip()
        if value != EXPECTED_VERSION:
            raise SystemExit(f"version mismatch in {name}: {value!r} != {EXPECTED_VERSION!r}")

    if final_bundle:
        for name in FINAL_BUNDLE_FILES - {"RC_BUILD.txt", "SHA256SUMS"}:
            require_file(root, name, 128)
        check_combined_manifest(root, packages)
        check_build_provenance(root, args.final_commit, args.final_run)

    for arch in ("amd64", "arm64"):
        portable = require_file(root, f"IServ-reMarkable-linux-{arch}.tar.gz")
        names = safe_tar_members(portable)
        prefix = f"IServ-reMarkable-linux-{arch}/"
        required = {
            prefix + "iserv-remarkable-setup",
            prefix + "iserv-rm-folder-helper",
            prefix + "README.md",
            prefix + "THIRD_PARTY_NOTICES.md",
            prefix + "SECURITY.md",
            prefix + "PRIVACY.md",
        }
        missing = sorted(required - names)
        if missing:
            raise SystemExit(f"Linux portable archive missing: {missing}")

    source = require_file(root, "Corresponding-Source-remarquee-rmapi.tar.gz")
    source_names = safe_tar_members(source)
    required_fragments = (
        "Corresponding-Source-remarquee-rmapi/BUILDING.md",
        "Corresponding-Source-remarquee-rmapi/Dockerfile",
        "Corresponding-Source-remarquee-rmapi/rmapi/LICENSE",
        "Corresponding-Source-remarquee-rmapi/remarquee/LICENSE",
        "Corresponding-Source-remarquee-rmapi/helper-rmfolder/main.go",
        "Corresponding-Source-remarquee-rmapi/helper-rmfolder/go.mod",
        "Corresponding-Source-remarquee-rmapi/helper-rmfolder/go.sum",
    )
    for name in required_fragments:
        if name not in source_names:
            raise SystemExit(f"corresponding source missing {name}")

    # Exact hosted-runner work paths should never be embedded in produced files.
    forbidden_path_markers = (
        b"/home/runner/work/",
        b"/Users/runner/work/",
        b"C:\\Users\\runneradmin\\",
        b"C:/Users/runneradmin/",
    )
    for path in root.iterdir():
        if not path.is_file() or path.suffix == ".sha256":
            continue
        data = path.read_bytes()
        for marker in forbidden_path_markers:
            if marker in data:
                raise SystemExit(f"runner path leaked into {path.name}")

    kind = "final RC bundle" if final_bundle else "RC package set"
    print(f"{kind} audit passed for {EXPECTED_VERSION}: {len(packages)} packages verified")


if __name__ == "__main__":
    main()
