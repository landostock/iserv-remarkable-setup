#!/usr/bin/env python3
"""Read the product version from cmd/setup/version.go and render package forms."""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
TEXT = (ROOT / "cmd/setup/version.go").read_text(encoding="utf-8")
match = re.search(r'^var appVersion = "([^"]+)"$', TEXT, re.MULTILINE)
if not match:
    raise SystemExit("appVersion not found")
version = match.group(1)
sem = re.fullmatch(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-rc(0|[1-9]\d*))?", version)
if not sem:
    raise SystemExit(f"unsupported release version: {version}")
major, minor, patch, rc = sem.groups()

forms = {
    "app": version,
    "deb": version.replace("-rc", "~rc"),
    "numeric4": f"{major}.{minor}.{patch}.{rc or '0'}",
    # CFBundleVersion is kept numeric and deterministic for the source version.
    "macbuild": f"{major}{int(minor):02d}{int(patch):02d}{int(rc or 0):03d}",
}

if len(sys.argv) != 2 or sys.argv[1] not in forms:
    raise SystemExit("usage: packaging/version.py app|deb|numeric4|macbuild")
print(forms[sys.argv[1]])
