#!/usr/bin/env python3
"""Cheap source-level release definition checks run before expensive RC gates."""

from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github/workflows/release-candidate.yml"
text = WORKFLOW.read_text(encoding="utf-8")

approved_actions = {
    "actions/checkout": "3d3c42e5aac5ba805825da76410c181273ba90b1",  # v7.0.1
    "actions/download-artifact": "3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c",  # v8.0.1
    "actions/setup-go": "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e",  # v7.0.0
    "actions/upload-artifact": "043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",  # v7.0.1
}
for workflow in sorted((ROOT / ".github/workflows").glob("*.yml")):
    workflow_text = workflow.read_text(encoding="utf-8")
    for line_number, line in enumerate(workflow_text.splitlines(), start=1):
        match = re.search(r"\buses:\s+([^\s#]+)", line)
        action = match.group(1) if match else ""
        if (
            action
            and not action.startswith("./")
            and not re.fullmatch(r"[^@\s]+@[0-9a-f]{40}", action)
        ):
            raise SystemExit(
                f"external action must use a full commit pin: "
                f"{workflow.relative_to(ROOT)}:{line_number}"
            )
        if action and not action.startswith("./"):
            name, _, commit = action.rpartition("@")
            approved_commit = approved_actions.get(name)
            if approved_commit and commit != approved_commit:
                raise SystemExit(
                    f"workflow action does not use its approved Node 24 pin: "
                    f"{workflow.relative_to(ROOT)}:{line_number}"
                )

required_workflow_markers = [
    "workflow_dispatch:",
    "source:",
    "linux:",
    "windows:",
    "macos:",
    "audit:",
    "packaging/linux/build.sh",
    "packaging/windows/build.ps1",
    "packaging/macos/build.sh",
    "packaging/audit_artifacts.py",
    "TestSafeParseToken",
    "govulncheck@v1.7.0",
    '--final-commit "$GITHUB_SHA"',
    '--final-run "$GITHUB_RUN_ID"',
    "actions/download-artifact@",
    "actions/upload-artifact@",
]
for marker in required_workflow_markers:
    if marker not in text:
        raise SystemExit(f"release workflow is incomplete: missing {marker!r}")

if "pull_request:" in text:
    raise SystemExit("expensive release workflow must not run for ordinary pull requests")

wrangler = (ROOT / "cmd/setup/payload/worker/wrangler.jsonc.template").read_text(encoding="utf-8")
if '"main": "src/guarded_index.js"' not in wrangler:
    raise SystemExit("release Worker must use the server-side activation guard entrypoint")
if '"workers_dev": true' not in wrangler or '"preview_urls": false' not in wrangler:
    raise SystemExit("release Worker must expose only its stable workers.dev endpoint")

bootstrap_config = json.loads(
    (ROOT / "cmd/setup/payload/worker/wrangler.bootstrap.jsonc.template").read_text(
        encoding="utf-8"
    )
)
if bootstrap_config != {
    "$schema": "node_modules/wrangler/config-schema.json",
    "name": "__WORKER_NAME__",
    "main": "src/bootstrap.js",
    "compatibility_date": "2026-08-18",
    "workers_dev": False,
    "preview_urls": False,
}:
    raise SystemExit("bootstrap Worker config must remain private, inert, and minimal")
bootstrap_source = (ROOT / "cmd/setup/payload/worker/src/bootstrap.js").read_text(
    encoding="utf-8"
)
for marker in ['status: 503', '"Cache-Control": "no-store"', '"X-Content-Type-Options": "nosniff"']:
    if marker not in bootstrap_source:
        raise SystemExit(f"bootstrap Worker is incomplete: missing {marker!r}")
if "scheduled" in bootstrap_source:
    raise SystemExit("bootstrap Worker must not contain a scheduled handler")

installer = (ROOT / "packaging/windows/installer.iss").read_text(encoding="utf-8")
version_source = (ROOT / "cmd/setup/version.go").read_text(encoding="utf-8")
provision_source = (ROOT / "cmd/setup/provision.go").read_text(encoding="utf-8")
fly_org_source = (ROOT / "cmd/setup/fly_org.go").read_text(encoding="utf-8")
commands_source = (ROOT / "cmd/setup/commands.go").read_text(encoding="utf-8")
maintenance_source = (ROOT / "cmd/setup/maintenance.go").read_text(encoding="utf-8")
fly_auth_source = (ROOT / "cmd/setup/fly_machine_auth.go").read_text(encoding="utf-8")
setup_ui = (ROOT / "cmd/setup/web/index.html").read_text(encoding="utf-8")
worker_package = (ROOT / "cmd/setup/payload/worker/package.json").read_text(encoding="utf-8")
worker_lock = (ROOT / "cmd/setup/payload/worker/package-lock.json").read_text(encoding="utf-8")
for dependency_file, label in [(worker_package, "package"), (worker_lock, "lockfile")]:
    if '"wrangler": "4.160.0"' not in dependency_file:
        raise SystemExit(f"Worker {label} must pin the audited Wrangler 4.160.0")
sync_source = (ROOT / "cmd/setup/payload/sync_job.py").read_text(encoding="utf-8")
for marker in [
    "class RemarqueeCommandError",
    'reason = "remarkable_" + exc.reason',
    "Gleichnamiges reMarkable-Dokument bleibt erhalten",
    "weitere Versuche wurden sicher angehalten",
]:
    if marker not in sync_source:
        raise SystemExit(f"reMarkable upload recovery is incomplete: missing {marker!r}")
for marker in [
    "REMARKABLE_AUTH_URLS = (",
    "https://webapp.cloud.remarkable.com/token/json/2/user/new",
    "https://webapp-prod.cloud.remarkable.engineering/token/json/2/user/new",
    "valid_cached_user_token",
    "JWT_SEGMENT_RE.fullmatch",
    "ambiguous_write=True",
    "REMARKABLE_HASH_RE.fullmatch(document_hash)",
]:
    if marker not in sync_source:
        raise SystemExit(f"native reMarkable import hardening is incomplete: missing {marker!r}")
for marker in [
    "class RcloneCommandError",
    "RCLONE_TRANSIENT_RETRY_DELAYS_SECONDS = (3, 8)",
    'return SyncRunResult(make_stats(), dry_run, "failed", "iserv_" + exc.reason, message)',
    "Datei- und Ordnernamen werden nicht protokolliert",
]:
    if marker not in sync_source:
        raise SystemExit(f"IServ transfer recovery is incomplete: missing {marker!r}")
if '"--force"' in sync_source:
    raise SystemExit("sync job must not overwrite existing reMarkable documents")
first_real_source = (ROOT / "cmd/setup/first_real_sync.go").read_text(encoding="utf-8")
for marker in [
    'document.querySelector("#maintenanceActivationSlot")',
    "dashboard.Activation.RealRunVerified",
    "firstRealActivationVerified(state, current)",
    "cloudComponentsCurrent(meta)",
    "if !a.requireCurrentCloudComponents(w, r, meta)",
]:
    if marker not in first_real_source:
        raise SystemExit(f"maintenance activation recovery is incomplete: missing {marker!r}")
for marker in [
    'id="maintenanceActivationSlot"',
    "lädt aber keine Datei hoch und schaltet die Automatik nicht frei",
    "uploads no files and does not unlock automation",
    "d.version_current===true",
    "Cloud-Komponenten aktualisieren",
]:
    if marker not in setup_ui:
        raise SystemExit(f"maintenance activation UI is incomplete: missing {marker!r}")
for marker in [
    'return "ISERV_RM_CONFIG_" + suffix',
    '[]string{"kv", "namespace", "list"}',
    '[]string{"kv", "namespace", "create", title}',
    "ensureKVNamespaceBinding",
    "Cloudflare-CONFIG-Bindung verweist auf einen anderen Speicher",
    "randomHex(8)",
    'setInstallProgress(10, "Cloudflare-Bereitschaft wird geprüft',
    "cloudflareEmailVerificationRequired",
    "Kein Code und keine Neuinstallation sind erforderlich",
]:
    if marker not in provision_source:
        raise SystemExit(
            f"installation-scoped resumable Cloudflare KV setup is incomplete: missing {marker!r}"
        )
if '"kv", "namespace", "create", "ISERV_RM_CONFIG"' in provision_source:
    raise SystemExit("release setup must not use the account-global legacy KV title")
worker_source = (ROOT / "cmd/setup/payload/worker/src/index.js").read_text(encoding="utf-8")
if "return value;" not in worker_source or 'return `Bearer ${value}`' in worker_source:
    raise SystemExit("Worker must preserve the complete FlyV1 Authorization header")
for marker in [
    'if (!startResponse.ok && startResponse.status !== 409)',
    'isFlyMachineTransition(startResponse.status, body)',
    'error.code = "fly_machine_transition"',
]:
    if marker not in worker_source:
        raise SystemExit(f"Worker Fly replacement handling is incomplete: missing {marker!r}")
for marker in [
    'if j.needsFlyDeploy() {',
    'j.FlyImageVersion = appVersion',
    "singleFlyMachineID(out)",
    "ensureWorkerFlyTarget(wrangler, j.AppName, j.MachineID)",
]:
    if marker not in provision_source:
        raise SystemExit(f"resumable Fly deployment handling is incomplete: missing {marker!r}")
if provision_source.index("setDashboardSettings(j.DashboardURL") >= provision_source.index("finalSecrets :="):
    raise SystemExit("safe selected settings must be published before connecting the Fly job")
if "validateFlyMachineControlToken(j.AppName, j.MachineID, flyToken)" not in provision_source:
    raise SystemExit("setup must validate the exact Fly control token before Worker publication")
if "validateFlyMachineControlToken(meta.AppName, meta.MachineID, flyToken)" not in maintenance_source:
    raise SystemExit("maintenance must validate a rotated Fly control token before Worker publication")
for marker in [
    'req.Header.Set("Authorization", token)',
    "validFlyControlToken(token)",
    'strings.Split(strings.TrimPrefix(value, "FlyV1 "), ",")',
    "http.ErrUseLastResponse",
]:
    if marker not in fly_auth_source:
        raise SystemExit(f"Fly control-token preflight is incomplete: missing {marker!r}")
if provision_source.index(
    "validateFlyMachineControlToken(j.AppName, j.MachineID, flyToken)"
) >= provision_source.index('a.putWranglerSecret(workerDir, "FLY_API_TOKEN", flyToken)'):
    raise SystemExit("setup must validate the Fly control token before publishing it")
if maintenance_source.index(
    "validateFlyMachineControlToken(meta.AppName, meta.MachineID, flyToken)"
) >= maintenance_source.index('a.putWranglerSecret(workerDir, "FLY_API_TOKEN", flyToken)'):
    raise SystemExit("maintenance must validate the Fly control token before publishing it")
cloudflare_readiness = provision_source.index(
    'setInstallProgress(10, "Cloudflare-Bereitschaft wird geprüft'
)
fly_provisioning = provision_source.index(
    'setInstallProgress(15, "Fly.io wird eingerichtet'
)
if cloudflare_readiness >= fly_provisioning:
    raise SystemExit("Cloudflare readiness must be proven before Fly provisioning")
worker_bootstrap = 'if !j.Steps["worker_bootstrap"] {'
if provision_source.count(worker_bootstrap) != 1:
    raise SystemExit("Cloudflare readiness must have exactly one journaled bootstrap step")
worker_bootstrap_offset = provision_source.index(worker_bootstrap)
for marker in [
    "flyAppCreateArgs(j.AppName, organization)",
    '[]string{"volumes", "create"',
    '[]string{"secrets", "import"',
    '[]string{"deploy", "--ha=false"',
]:
    if worker_bootstrap_offset >= provision_source.index(marker):
        raise SystemExit(
            f"Cloudflare readiness must precede Fly provisioning marker {marker!r}"
        )
for marker in [
    '[]string{"orgs", "list", "--json"}',
    "decodeFlyOrganizations(organizationOutput)",
    "chooseFlyOrganization(organizations, cfg.FlyOrgSlug)",
]:
    if marker not in provision_source:
        raise SystemExit(f"Fly organization provisioning is incomplete: missing {marker!r}")
for marker in [
    "maxFlyOrganizationsOutput = 64 * 1024",
    "flyOrgSlugPattern",
    '[]string{"apps", "create", appName, "--org", organization, "--yes"}',
    "die ausgewählte Fly-Organisation ist nicht mehr verfügbar",
]:
    if marker not in fly_org_source:
        raise SystemExit(f"Fly organization validation is incomplete: missing {marker!r}")
for marker in ["flyOrgSelect", "fly_org_slug", "renderFlyOrganizations"]:
    if marker not in setup_ui:
        raise SystemExit(f"Fly organization selection UI is incomplete: missing {marker!r}")
for marker in [
    "Neues Cloudflare-Konto?",
    "Cloudflares Bestätigungs-E-Mail",
    "Im Installer muss dafür kein Code eingegeben werden",
]:
    if marker not in setup_ui:
        raise SystemExit(
            f"Cloudflare account prerequisite guidance is incomplete: missing {marker!r}"
        )
for marker in ["redactHomePaths(cleanCommandOutput(value))", "/Users|/home", "Users\\\\"]:
    if marker not in commands_source:
        raise SystemExit(f"portable provider-log path redaction is incomplete: missing {marker!r}")
version_match = re.search(r'^var appVersion = "(\d+)\.(\d+)\.(\d+)-rc(\d+)"$', version_source, re.MULTILINE)
if not version_match:
    raise SystemExit("setup version must use the supported release-candidate format")
major, minor, patch, rc = version_match.groups()
app_version = f"{major}.{minor}.{patch}-rc{rc}"
numeric_version = f"{major}.{minor}.{patch}.{rc}"
if f'#define Version "{app_version}"' not in installer:
    raise SystemExit("Windows installer fallback version differs from the setup version")
if f'#define NumericVersion "{numeric_version}"' not in installer:
    raise SystemExit("Windows installer numeric fallback differs from the setup version")
if "PrivilegesRequired=lowest" not in installer:
    raise SystemExit("Windows installer must remain per-user by default")
if "PrivilegesRequiredOverridesAllowed=dialog" in installer:
    raise SystemExit("Windows installer must not invite elevation through the normal wizard")
if "SetupIconFile" not in installer:
    raise SystemExit("Windows installer icon integration is missing")

windows_build = (ROOT / "packaging/windows/build.ps1").read_text(encoding="utf-8")
for marker in [
    '"/DIR=`"$smokeDir`""',
    '"/LOG=`"$smokeLog`""',
    "Start-Process",
    "-Wait -PassThru",
    '[IO.File]::WriteAllText("$installer.sha256"',
    "[Text.UTF8Encoding]::new($false)",
    "$missingInstalled",
    "$remainingFiles",
]:
    if marker not in windows_build:
        raise SystemExit(f"Windows installer smoke test is incomplete: missing {marker!r}")

plist = (ROOT / "packaging/macos/Info.plist.in").read_text(encoding="utf-8")
if "CFBundleIconFile" not in plist or "AppIcon" not in plist:
    raise SystemExit("macOS app icon metadata is missing")

for path in [
    ROOT / "packaging/linux/build.sh",
    ROOT / "packaging/windows/build.ps1",
    ROOT / "packaging/macos/build.sh",
    ROOT / "packaging/audit_artifacts.py",
    ROOT / "assets/app-icon.svg",
    ROOT / "helper/rmfolder/go.sum",
    ROOT / "cmd/setup/payload/rmapi_safe_api.go.txt",
    ROOT / "cmd/setup/payload/rmapi_safe_api_test.go.txt",
]:
    if not path.is_file() or path.stat().st_size == 0:
        raise SystemExit(f"required release input missing: {path.relative_to(ROOT)}")

helper_mod = (ROOT / "helper/rmfolder/go.mod").read_text(encoding="utf-8")
if not helper_mod.startswith("module github.com/landostock/iserv-remarkable/helper/rmfolder\n"):
    raise SystemExit("folder helper must use the repository-owned Go module path")
if "replace github.com/juruen/rmapi => github.com/FNStudios-NI/rmapi" not in helper_mod:
    raise SystemExit("folder helper must pin the audited rmapi fork through go.mod")
if "f295d5466978" not in helper_mod:
    raise SystemExit("folder helper rmapi fork pin does not match the release source pin")
if "\ngo 1.26.11\n" not in helper_mod:
    raise SystemExit("folder helper must build with the vulnerability-fixed Go toolchain")
full_rmapi_commit = "f295d5466978954edabaae4c730d50d03740cfac"

helper_source = (ROOT / "helper/rmfolder/main.go").read_text(encoding="utf-8")
native_smoke = (ROOT / ".github/workflows/native-smoke.yml").read_text(encoding="utf-8")
if 'os.Args[1] == "--self-test"' not in helper_source:
    raise SystemExit("folder helper self-test entrypoint is missing")
if "rmfolder-native${{ matrix.exe }} --self-test" not in native_smoke:
    raise SystemExit("native smoke does not execute the folder helper")
if full_rmapi_commit not in native_smoke:
    raise SystemExit("native smoke does not pin the full rmapi source commit")

arch_build = (ROOT / "packaging/arch/build.sh").read_text(encoding="utf-8")
arch_install = (ROOT / "packaging/arch/install.sh").read_text(encoding="utf-8")
arch_uninstall = (ROOT / "packaging/arch/uninstall.sh").read_text(encoding="utf-8")
arch_test = (ROOT / "packaging/arch/test_installer.sh").read_text(encoding="utf-8")
arch_readme = (ROOT / "packaging/arch/README.md").read_text(encoding="utf-8")
for marker in [
    full_rmapi_commit,
    "CGO_ENABLED=0 GOOS=linux GOARCH=\"$arch\"",
    "rmapi_safe_api.go.txt",
    "droprequire=github.com/golang-jwt/jwt",
    "bash packaging/arch/test_installer.sh",
    "find . -type f ! -name SHA256SUMS",
]:
    if marker not in arch_build:
        raise SystemExit(f"Arch package build is incomplete: missing {marker!r}")
for marker in [
    "sha256sum --strict --check SHA256SUMS",
    "iserv-rm-folder-helper\" --self-test",
    "0.9.0-rc27",
    '$HOME/.local/bin',
    "Terminal=false",
]:
    if marker not in arch_install:
        raise SystemExit(f"Arch installer is incomplete: missing {marker!r}")
if "IServ-reMarkable-Setup" in arch_uninstall and "STATE_DIR" not in arch_uninstall:
    raise SystemExit("Arch uninstaller must identify preserved setup state")
for marker in ["state-marker", "tampered payload was accepted", "Arch installer tests passed"]:
    if marker not in arch_test:
        raise SystemExit(f"Arch installer regression test is incomplete: missing {marker!r}")
for marker in ["0.9.0-rc27", "sha256sum --check", "Automatik AUS", "RC27 überinstallieren"]:
    if marker not in arch_readme:
        raise SystemExit(f"Arch installation guide is incomplete: missing {marker!r}")
if '"github.com/juruen/rmapi/api/sync15"' not in helper_source:
    raise SystemExit("folder helper must call the pinned sync API directly")
if "api.ParseToken" in helper_source or '"github.com/juruen/rmapi/api"' in helper_source:
    raise SystemExit("folder helper must not use the vulnerable legacy JWT parser")
for marker in [
    "readRetryTransport",
    "req.Method != http.MethodGet",
    "http.StatusServiceUnavailable",
    '"connection reset by peer"',
    "req.Context().Done()",
]:
    if marker not in helper_source:
        raise SystemExit(f"folder helper read retry is incomplete: missing {marker!r}")

safe_rmapi = (ROOT / "cmd/setup/payload/rmapi_safe_api.go.txt").read_text(encoding="utf-8")
safe_rmapi_test = (ROOT / "cmd/setup/payload/rmapi_safe_api_test.go.txt").read_text(encoding="utf-8")
for marker in [
    "const maxUserTokenBytes = 64 << 10",
    'strings.Split(userToken, ".")',
    "base64.RawURLEncoding.DecodeString",
    "len(parts) != 3",
    "time.Now().Unix() > claims.ExpiresAt",
]:
    if marker not in safe_rmapi:
        raise SystemExit(f"bounded rmapi token parser is incomplete: missing {marker!r}")
for forbidden in ["github.com/golang-jwt/jwt", "ParseUnverified", "StandardClaims"]:
    if forbidden in safe_rmapi:
        raise SystemExit(f"bounded rmapi token parser retains forbidden code: {forbidden!r}")
for marker in [
    "TestSafeParseTokenAcceptsBoundedCurrentClaims",
    "TestSafeParseTokenRejectsExpiredClaims",
    "TestSafeParseTokenRejectsMalformedClaims",
    "TestSafeParseTokenRejectsOversizedInput",
]:
    if marker not in safe_rmapi_test:
        raise SystemExit(f"bounded rmapi token tests are incomplete: missing {marker!r}")

for script in [
    ROOT / "packaging/linux/build.sh",
    ROOT / "packaging/macos/build.sh",
    ROOT / "packaging/windows/build.ps1",
    ROOT / "packaging/source/build-corresponding-source.sh",
]:
    script_text = script.read_text(encoding="utf-8")
    if "go.sum" not in script_text:
        raise SystemExit(f"folder helper go.sum is not carried by {script.relative_to(ROOT)}")
    if full_rmapi_commit not in script_text:
        raise SystemExit(f"full rmapi source commit is not pinned by {script.relative_to(ROOT)}")
    if "rmapi_safe_api.go.txt" not in script_text:
        raise SystemExit(f"bounded rmapi parser is not carried by {script.relative_to(ROOT)}")
    if "droprequire=github.com/golang-jwt/jwt" not in script_text:
        raise SystemExit(f"legacy JWT dependency is not removed by {script.relative_to(ROOT)}")

source_script = (ROOT / "packaging/source/build-corresponding-source.sh").read_text(encoding="utf-8")
if "Go security floor: 1.26.11" not in source_script:
    raise SystemExit("corresponding-source build notes omit the Go security floor")
for marker in [
    'case "$OUTPUT" in',
    'OUTPUT="$(pwd -P)/$OUTPUT"',
    'OUTPUT="$(cd "$(dirname "$OUTPUT")" && pwd -P)/$(basename "$OUTPUT")"',
]:
    if marker not in source_script:
        raise SystemExit(f"corresponding-source output path handling is incomplete: missing {marker!r}")

dockerfile = (ROOT / "cmd/setup/payload/Dockerfile").read_text(encoding="utf-8")
for marker in [
    full_rmapi_commit,
    "golang:1.26.11-bookworm@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0",
    "python:3.13-slim-bookworm@sha256:a5a7017efc5969e00ae2b91c8eb0631049a569652aa4ffc4501769049ae45c72",
    "COPY rmapi_safe_api.go.txt /tmp/rmapi_safe_api.go",
    "cp /tmp/rmapi_safe_api.go api/api.go",
    "go mod edit -droprequire=github.com/golang-jwt/jwt",
    "go mod tidy",
    "go get google.golang.org/grpc@v1.83.1 golang.org/x/net@v0.60.0",
]:
    if marker not in dockerfile:
        raise SystemExit(f"Fly image rmapi hardening is incomplete: missing {marker!r}")

ci_workflow = (ROOT / ".github/workflows/ci.yml").read_text(encoding="utf-8")
for marker in ["govulncheck@v1.7.0", 'govulncheck" ./...', "helper/rmfolder"]:
    if marker not in ci_workflow:
        raise SystemExit(f"permanent Go vulnerability checks are incomplete: missing {marker!r}")

macos_script = (ROOT / "packaging/macos/build.sh").read_text(encoding="utf-8")
macos_info = (ROOT / "packaging/macos/Info.plist.in").read_text(encoding="utf-8")
macos_readme = (ROOT / "packaging/macos/README.md").read_text(encoding="utf-8")
if "lipo -verify_arch" in macos_script:
    raise SystemExit("macOS package script must pass the binary path before lipo -verify_arch")
for binary in ["iserv-remarkable-setup", "iserv-rm-folder-helper"]:
    for arch in ["x86_64", "arm64"]:
        marker = f'lipo "$CONTENTS/MacOS/{binary}" -verify_arch {arch}'
        if marker not in macos_script:
            raise SystemExit(f"macOS package must verify {binary} for {arch}")
if "iserv-rm-folder-helper\" --self-test" not in macos_script:
    raise SystemExit("macOS package does not execute its mounted folder helper")
for marker in ["detach_dmg()", 'hdiutil detach -force "$MOUNT"']:
    if marker not in macos_script:
        raise SystemExit(f"macOS package lacks bounded DMG cleanup: missing {marker!r}")
if 'packaging/macos/README.md "$CONTENTS/Resources/INSTALLATION.md"' not in macos_script:
    raise SystemExit("macOS package does not carry its installation guide")
if "<key>LSMinimumSystemVersion</key>\n  <string>13.5</string>" not in macos_info:
    raise SystemExit("macOS package minimum must match the bundled Cloudflare runtime")
for marker in ["0.9.0-rc27", "macOS 13.5", "shasum -a 256 -c", "Automatik AUS", "RC27 überinstallieren"]:
    if marker not in macos_readme:
        raise SystemExit(f"macOS installation guide is incomplete: missing {marker!r}")

linux_script = (ROOT / "packaging/linux/build.sh").read_text(encoding="utf-8")
if 'iserv-rm-folder-helper" --self-test' not in linux_script:
    raise SystemExit("Linux package does not execute its native folder helper")
if "go mod edit '-droprequire=github.com/golang-jwt/jwt'" not in windows_build:
    raise SystemExit("Windows package does not quote the JWT removal argument for PowerShell")
if "installed x64 folder helper self-test failed" not in windows_build:
    raise SystemExit("Windows package does not execute its installed folder helper")

remarkable_browser = (ROOT / "cmd/setup/remarkable_browser.go").read_text(encoding="utf-8")
fly_template = (ROOT / "cmd/setup/payload/fly.toml.template").read_text(encoding="utf-8")
for marker in ['MAX_UPLOADS_PER_RUN = "5"', 'REMARQUEE_MIN_INTERVAL_SECONDS = "2.5"', 'RMAPI_CONCURRENT = "2"']:
    if marker not in fly_template:
        raise SystemExit(f"Fly sync safety configuration is incomplete: missing {marker!r}")
for marker in [
    "remarkableFolderTimeout     = 8 * time.Minute",
    'remarkableFolderConcurrency = "2"',
    'remarkableFolderAttemptConcurrency',
    'isTransientRemarkableFolderError',
    '"connection reset by peer"',
    "time.Until(deadline)",
    'os.MkdirTemp(workDir, "remarkable-helper-")',
    '"XDG_CACHE_HOME"',
    '"XDG_CONFIG_HOME"',
    '"LOCALAPPDATA"',
    '"APPDATA"',
    "context.DeadlineExceeded",
    "func() error",
    "could not be removed securely",
    'filepath.Join(helperEnv["HOME"], "rmapi-preview.conf")',
]:
    if marker not in remarkable_browser:
        raise SystemExit(f"reMarkable folder loading hardening is incomplete: missing {marker!r}")

setup_main = (ROOT / "cmd/setup/main.go").read_text(encoding="utf-8")
if "//go:embed web payload" in setup_main:
    raise SystemExit("setup companion must not broadly embed generated payload directories")
if "payload/worker/src/*.js" not in setup_main:
    raise SystemExit("setup companion embed allowlist is missing Worker runtime sources")
if "payload/worker/wrangler.bootstrap.jsonc.template" not in setup_main:
    raise SystemExit("setup companion embed allowlist is missing the bootstrap Worker config")
if "payload/rmapi_safe_api.go.txt" not in setup_main:
    raise SystemExit("setup companion embed allowlist is missing the bounded rmapi parser")

provision_source = (ROOT / "cmd/setup/provision.go").read_text(encoding="utf-8")
bootstrap_deploy = "wranglerDeployArgs(filepath.Base(bootstrapWrangler))"
secret_put = '[]string{"secret", "put", name, "--x-auto-create=false"}'
for marker in [
    'j.Steps["worker_bootstrap"]',
    bootstrap_deploy,
    secret_put,
    '[]string{"deploy", "--autoconfig=false", "--x-auto-create=false"}',
    "cloudflareWorkersOnboardingURL",
]:
    if marker not in provision_source:
        raise SystemExit(f"safe Worker provisioning is incomplete: missing {marker!r}")
if provision_source.index(bootstrap_deploy) > provision_source.index("wranglerSecretArgs(name)"):
    raise SystemExit("bootstrap Worker must be deployed before any Worker secret is set")

maintenance_source = (ROOT / "cmd/setup/maintenance.go").read_text(encoding="utf-8")
if 'runWranglerIn(workerDir, wranglerDeployArgs("")' not in maintenance_source:
    raise SystemExit("maintenance Worker deploy must use deterministic resource settings")
for marker in ["enforceWorkerPublicationPolicy", '{key: "workers_dev", value: "true"}', '{key: "preview_urls", value: "false"}']:
    if marker not in maintenance_source:
        raise SystemExit(f"resumed Worker publication policy migration is missing: {marker!r}")

commands_source = (ROOT / "cmd/setup/commands.go").read_text(encoding="utf-8")
for marker in ["commandEscapePatterns", "cleanCommandOutput(value)"]:
    if marker not in commands_source:
        raise SystemExit(f"command-output terminal sanitization is missing: {marker!r}")

root_mod = (ROOT / "go.mod").read_text(encoding="utf-8")
if "\ngo 1.26.11\n" not in root_mod:
    raise SystemExit("setup release must build with the vulnerability-fixed Go toolchain")

fly_tokens = (ROOT / "cmd/setup/fly_tokens.go").read_text(encoding="utf-8")
for marker in [
    '"tokens", "list"',
    '"tokens", "revoke"',
    "flyControlTokenName",
    "flyTokenRevokeSucceeded",
]:
    if marker not in fly_tokens:
        raise SystemExit(f"Fly control-token rotation is incomplete: missing {marker!r}")

fly_auth = (ROOT / "cmd/setup/fly_auth.go").read_text(encoding="utf-8")
setup_ui = (ROOT / "cmd/setup/web/index.html").read_text(encoding="utf-8")
for marker in ["flyCodeSubmission", "redeemFlySession", "broker.submissions"]:
    if marker not in fly_auth:
        raise SystemExit(f"Fly completion-code fallback is incomplete: missing {marker!r}")
for marker in ['id="flyCode"', 'id="flyCodeSubmit"', '"/api/fly/code"']:
    if marker not in setup_ui:
        raise SystemExit(f"Fly completion-code UI is incomplete: missing {marker!r}")

cloudflare_auth = (ROOT / "cmd/setup/cloudflare_auth.go").read_text(encoding="utf-8")
for marker in [
    '[]string{"whoami", "--json"}',
    "identity.LoggedIn",
    'cloudflareAuthDirectory = "cloudflare-home-v2"',
    'filepath.Join(config, ".wrangler", "config")',
    "os.CreateTemp(authConfig",
    '"XDG_CONFIG_HOME"',
    '"CLOUDFLARE_API_TOKEN"',
    '"CLOUDFLARE_AUTH_USE_KEYRING"',
]:
    if marker not in cloudflare_auth:
        raise SystemExit(f"Cloudflare authentication hardening is incomplete: missing {marker!r}")

setup_app = (ROOT / "cmd/setup/app.go").read_text(encoding="utf-8")
if "remarkableFolderMu sync.Mutex" not in setup_app or setup_app.count("a.remarkableFolderMu.Lock()") < 2:
    raise SystemExit("reMarkable account and folder-cache transitions are not serialized")
if '[]string{"login", "--no-use-keyring"}' not in setup_app:
    raise SystemExit("Cloudflare login must explicitly disable shared keyring storage")

for path in [
    ROOT / "cmd/setup/web/index.html",
    ROOT / "cmd/setup/payload/worker/src/dashboard.js",
    ROOT / "cmd/setup/payload/worker/src/login.js",
]:
    source = path.read_text(encoding="utf-8")
    values = re.findall(r"letter-spacing\s*:\s*([^;}]+)", source)
    if any(value.strip() != "0" for value in values):
        raise SystemExit(f"non-zero letter spacing remains in {path.relative_to(ROOT)}")

worker_package = json.loads((ROOT / "cmd/setup/payload/worker/package.json").read_text(encoding="utf-8"))
worker_lock = json.loads((ROOT / "cmd/setup/payload/worker/package-lock.json").read_text(encoding="utf-8"))
wrangler_version = worker_package["devDependencies"].get("wrangler", "")
if not re.fullmatch(r"\d+\.\d+\.\d+", wrangler_version):
    raise SystemExit("Wrangler must be pinned to an exact semver version in package.json")
lock_root = worker_lock["packages"][""].get("devDependencies", {}).get("wrangler")
lock_package = worker_lock["packages"].get("node_modules/wrangler", {}).get("version")
if lock_root != wrangler_version or lock_package != wrangler_version:
    raise SystemExit("Wrangler package.json and package-lock.json pins disagree")

print("Release definition source checks passed")
