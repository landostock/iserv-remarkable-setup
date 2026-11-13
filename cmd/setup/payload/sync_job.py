#!/usr/bin/env python3
"""One-shot IServ WebDAV -> reMarkable Cloud sync job.

The job is deliberately conservative: local state corruption fails closed,
cloud-control requests never follow redirects, uploads are rate-limited, and
cloud logs avoid document names and school folder paths.
"""

from __future__ import annotations

import base64
import hashlib
import http.client
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path, PurePosixPath
from typing import Any


DATA_DIR = Path(os.environ.get("DATA_DIR", "/data"))
CONFIG_PATH = Path(os.environ.get("SYNC_CONFIG", "/app/config.json"))
MANIFEST_PATH = DATA_DIR / "manifest.json"
STATE_PATH = DATA_DIR / "sync_state.json"
RMAPI_CONFIG_PATH = Path(os.environ.get("RMAPI_CONFIG", "/data/rmapi.conf"))
RMAPI_CONFIG_SEED_PATH = RMAPI_CONFIG_PATH.with_name(RMAPI_CONFIG_PATH.name + ".seed.sha256")
DEFAULT_DRY_RUN = os.environ.get("DRY_RUN", "true").lower() in {"1", "true", "yes", "on"}
MIN_RUNTIME_SECONDS = int(os.environ.get("MIN_RUNTIME_SECONDS", "12"))
MAX_UPLOADS_PER_RUN = max(1, int(os.environ.get("MAX_UPLOADS_PER_RUN", "5")))
RATE_LIMIT_COOLDOWN_SECONDS = max(60, int(os.environ.get("RATE_LIMIT_COOLDOWN_SECONDS", "1800")))
REMARQUEE_MIN_INTERVAL_SECONDS = max(0.0, float(os.environ.get("REMARQUEE_MIN_INTERVAL_SECONDS", "2.5")))
REMARQUEE_GENERATION_RETRY_DELAYS_SECONDS = (2, 5, 10)
REMARQUEE_PROTOCOL_RETRY_DELAYS_SECONDS = (2,)
REMARQUEE_TRANSIENT_RETRY_DELAYS_SECONDS = (3, 8)
REMARKABLE_READ_TIMEOUT_SECONDS = 8 * 60
RCLONE_TRANSIENT_RETRY_DELAYS_SECONDS = (3, 8)
CONTROL_URL = os.environ.get("CONTROL_URL", "").rstrip("/")
SYNC_API_TOKEN = os.environ.get("SYNC_API_TOKEN", "")
MAX_CONTROL_RESPONSE = 2 * 1024 * 1024
MAX_RMAPI_CONFIG_BYTES = 256 * 1024
MAX_REMARKABLE_AUTH_RESPONSE = 64 * 1024
MAX_REMARKABLE_UPLOAD_RESPONSE = 64 * 1024
MAX_REMARKABLE_UPLOAD_REDIRECTS = 3
REMARKABLE_AUTH_URLS = (
    "https://webapp.cloud.remarkable.com/token/json/2/user/new",
    "https://webapp-prod.cloud.remarkable.engineering/token/json/2/user/new",
)
REMARKABLE_UPLOAD_HOST = "internal.cloud.remarkable.com"
REMARKABLE_UPLOAD_PATH = "/doc/v2/files"
REMARKABLE_UPLOAD_HOST_SUFFIXES = (".remarkable.com", ".remarkable.engineering")
REMARKABLE_ID_RE = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
REMARKABLE_HASH_RE = re.compile(r"^[0-9a-f]{64}$", re.IGNORECASE)
JWT_SEGMENT_RE = re.compile(r"^[A-Za-z0-9_-]+$")
ANSI_ESCAPE_RE = re.compile(r"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))")


class SyncError(RuntimeError):
    pass


class RateLimitError(SyncError):
    pass


class RemarqueeCommandError(SyncError):
    def __init__(
        self,
        reason: str,
        message: str,
        *,
        diagnostic_code: str = "RM-UNCLASSIFIED",
        ambiguous_write: bool = False,
    ):
        super().__init__(message)
        self.reason = reason
        self.diagnostic_code = diagnostic_code
        self.ambiguous_write = ambiguous_write


class RcloneCommandError(SyncError):
    def __init__(self, reason: str, message: str):
        super().__init__(message)
        self.reason = reason


class UploadAttemptLimit(SyncError):
    pass


class NoRedirectHandler(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):  # noqa: N802
        return None


@dataclass
class UploadCandidate:
    source: dict[str, Any]
    path: Path
    relative: Path
    digest: str
    key: str
    previous: dict[str, Any] | None
    target_dir: str


@dataclass
class SyncRunResult:
    stats: dict[str, int]
    dry_run: bool
    outcome: str
    reason: str | None = None
    message: str | None = None
    cooldown_until: str | None = None


_last_remarquee_started = 0.0
_remarkable_user_token = ""
REMARKABLE_SESSION_SKEW_SECONDS = 300


def log(message: str) -> None:
    print(f"[{datetime.now().astimezone().isoformat(timespec='seconds')}] {message}", flush=True)


def run(
    command: list[str], *, check: bool = True, timeout: float | None = None,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    # stdout/stderr are intentionally captured but not emitted: rclone and cloud
    # clients can include document names or school folder paths in their output.
    try:
        result = subprocess.run(command, text=True, capture_output=True, timeout=timeout, env=env)
    except subprocess.TimeoutExpired:
        result = subprocess.CompletedProcess(command, 1, stdout="", stderr="operation timed out")
        result.timed_out = True
    if check and result.returncode != 0:
        raise SyncError(f"Befehl fehlgeschlagen ({result.returncode}): {Path(command[0]).name}")
    return result


def is_rate_limited_result(result: subprocess.CompletedProcess[str]) -> bool:
    output = normalized_command_output(result)
    return "status 429" in output or "429 too many requests" in output or "too many requests" in output


def normalized_command_output(result: subprocess.CompletedProcess[str]) -> str:
    output = f"{result.stdout or ''}\n{result.stderr or ''}"
    output = ANSI_ESCAPE_RE.sub("", output)
    return " ".join(output.lower().split())


def command_http_status(result: subprocess.CompletedProcess[str]) -> int | None:
    output = normalized_command_output(result)
    patterns = (
        r"(?:http(?:/\d(?:\.\d)?)?\s+|http(?: error)?\s+|status(?: code)?[=:\s]+)(\d{3})\b",
        r"\b(4\d{2}|5\d{2})\s+(?:bad request|unauthorized|forbidden|not found|conflict|precondition|required|too many requests|internal server error|bad gateway|service unavailable|gateway timeout)\b",
    )
    for pattern in patterns:
        match = re.search(pattern, output)
        if match:
            return int(match.group(1))
    return None


def classify_rclone_failure(result: subprocess.CompletedProcess[str]) -> str:
    output = f"{result.stdout or ''}\n{result.stderr or ''}".lower()
    if any(marker in output for marker in (
        "401 unauthorized", "status code 401", "http error 401", "403 forbidden",
        "status code 403", "http error 403", "authentication failed", "invalid credentials",
    )):
        return "auth"
    if any(marker in output for marker in (
        "404 not found", "status code 404", "http error 404", "directory not found",
        "couldn't find directory entry", "object not found",
    )):
        return "source"
    if any(marker in output for marker in (
        "x509:", "certificate signed by unknown authority", "certificate has expired",
        "certificate is not trusted", "tls: failed to verify certificate",
    )):
        return "certificate"
    if any(marker in output for marker in (
        "no space left on device", "disk quota exceeded", "read-only file system",
    )):
        return "storage"
    if any(marker in output for marker in (
        "connection reset", "i/o timeout", "timed out", "timeout", "temporary failure",
        "tls handshake timeout", "no such host", "network is unreachable", "connection refused",
        "unexpected eof", "server closed idle connection", "status code 408", "status code 429",
        "status code 500", "status code 502", "status code 503", "status code 504",
        "http error 408", "http error 429", "http error 500", "http error 502",
        "http error 503", "http error 504", "too many requests", "bad gateway",
        "service unavailable", "gateway timeout", "internal server error",
    )):
        return "network"
    return "provider"


def safe_rclone_failure_message(reason: str) -> str:
    messages = {
        "auth": "IServ hat den WebDAV-Zugriff abgelehnt. Prüfe oder erneuere die IServ-Zugangsdaten im Installer.",
        "source": "Ein ausgewählter IServ-Quellordner ist nicht mehr erreichbar. Prüfe die Ordnerzuordnung und deine Zugriffsrechte.",
        "certificate": "Die sichere Verbindung zu IServ konnte nicht verifiziert werden. Schalte die Zertifikatsprüfung nicht aus; prüfe die WebDAV-Adresse oder kontaktiere die Schule.",
        "storage": "Der geschützte Fly-Speicher ist voll oder nicht beschreibbar. Führe im Installer ein Cloud-Update aus und erstelle bei erneutem Auftreten einen Diagnosebericht.",
        "network": "Die Verbindung zu IServ wurde vorübergehend unterbrochen. Der Lauf wurde nach mehreren sicheren Versuchen angehalten; starte ihn später erneut.",
        "provider": "IServ WebDAV hat die Dateiübertragung unerwartet abgelehnt. Teste die IServ-Verbindung im Installer und starte den Lauf erneut.",
    }
    return messages.get(reason, messages["provider"])


def rclone_environment() -> dict[str, str] | None:
    encoded = os.environ.get("ISERV_WEBDAV_USER_B64")
    if encoded is None:
        return None  # Existing Fly installations use the original rclone secret.
    try:
        if len(encoded) > 344:  # At most 256 UTF-8 bytes from the installer.
            raise ValueError("invalid username")
        user = base64.b64decode(encoded, validate=True).decode("utf-8")
        if not user or len(user.encode("utf-8")) > 256 or any(ch in user for ch in "\x00\r\n"):
            raise ValueError("invalid username")
    except ValueError as exc:
        raise RcloneCommandError("auth", safe_rclone_failure_message("auth")) from exc
    env = dict(os.environ)
    env.pop("ISERV_WEBDAV_USER_B64")
    env["RCLONE_CONFIG_ISERV_USER"] = user
    return env


def run_rclone(command: list[str]) -> subprocess.CompletedProcess[str]:
    env = rclone_environment()
    for attempt in range(len(RCLONE_TRANSIENT_RETRY_DELAYS_SECONDS) + 1):
        result = run(command, check=False, env=env)
        if result.returncode == 0:
            return result
        reason = classify_rclone_failure(result)
        if reason != "network" or attempt >= len(RCLONE_TRANSIENT_RETRY_DELAYS_SECONDS):
            raise RcloneCommandError(reason, safe_rclone_failure_message(reason))
        delay = RCLONE_TRANSIENT_RETRY_DELAYS_SECONDS[attempt]
        log(f"IServ-Verbindung war vorübergehend unterbrochen; sicherer neuer Versuch in {delay} Sekunden.")
        time.sleep(delay)
    raise AssertionError("unreachable")


def classify_remarquee_failure(result: subprocess.CompletedProcess[str]) -> str:
    output = normalized_command_output(result)
    status = command_http_status(result)
    if "entry already exists" in output:
        return "conflict"
    if status in {401, 403}:
        return "auth"
    if status in {409, 412, 428}:
        return "generation"
    if status in {400, 404}:
        return "protocol"
    if status in {408, 425, 500, 502, 503, 504}:
        return "network"
    if status in {413, 415, 422}:
        return "document"
    if any(marker in output for marker in (
        "status 401", "401 unauthorized", "unauthorized", "status 403", "403 forbidden",
        "forbidden", "token expired", "invalid token", "device token",
    )):
        return "auth"
    if any(marker in output for marker in (
        "status 409", "409 conflict", "status=409", "status 412", "412 precondition",
        "status=412", "status 428", "428 precondition", "status=428",
        "wrong generation", "generation conflict",
    )):
        return "generation"
    if any(marker in output for marker in (
        "status 400", "400 bad request", "status=400", "invalid root schema", "invalid hash",
        "software must be updated", "unexpected 'rm-filename'", 'unexpected "rm-filename"',
    )):
        return "protocol"
    if any(marker in output for marker in ("directory doesn't exist", "no such directory")):
        return "target"
    if any(marker in output for marker in (
        "connection reset", "timed out", "timeout", "temporary failure", "tls handshake",
        "no such host", "network is unreachable", "connection refused", "unexpected eof",
        "broken pipe", "server misbehaving", "bad gateway", "service unavailable",
        "gateway timeout", "internal server error",
    )):
        return "network"
    if any(marker in output for marker in (
        "unsupported document", "unsupported file", "invalid pdf", "invalid epub",
        "could not parse", "cannot read",
    )):
        return "document"
    return "provider"


def safe_remarquee_failure_message(reason: str) -> str:
    messages = {
        "conflict": "Im reMarkable-Ziel ist bereits ein Dokument mit demselben Namen vorhanden.",
        "auth": "Die reMarkable-Cloud-Autorisierung ist abgelaufen oder wurde abgelehnt. Verbinde reMarkable im Installer erneut.",
        "target": "Der ausgewählte reMarkable-Zielordner ist nicht mehr verfügbar. Prüfe die Ordnerzuordnung.",
        "network": "Die Verbindung zur reMarkable Cloud wurde unterbrochen. Warte kurz und starte den Lauf erneut.",
        "timeout": "Das vollständige Lesen der reMarkable Cloud hat die Zeitgrenze erreicht. Prüfe die Verbindung und wiederhole den Lauf.",
        "document": "reMarkable hat das Dokument nicht akzeptiert. Prüfe, ob es eine gültige PDF- oder EPUB-Datei ist.",
        "generation": "Die reMarkable Cloud wurde parallel geändert. Der sichere Wiederholungsversuch konnte nicht abgeschlossen werden; warte, bis das Tablet vollständig synchronisiert ist, und starte den Lauf erneut.",
        "protocol": "Die reMarkable Cloud verwendet ein geändertes Sync-Protokoll. Aktualisiere die Cloud-Komponenten im Installer; erneutes Verbinden behebt diesen Fehler nicht.",
        "provider": "Die reMarkable Cloud hat den Upload unerwartet abgelehnt. Erstelle im Installer einen Diagnosebericht; erneutes Verbinden ist ohne Autorisierungsfehler nicht erforderlich.",
    }
    return messages.get(reason, messages["provider"])


def remarkable_error(
    reason: str,
    diagnostic_code: str,
    *,
    ambiguous_write: bool = False,
) -> RemarqueeCommandError:
    return RemarqueeCommandError(
        reason,
        safe_remarquee_failure_message(reason),
        diagnostic_code=diagnostic_code,
        ambiguous_write=ambiguous_write,
    )


def rmapi_tree_cache_path() -> Path:
    configured = os.environ.get("XDG_CACHE_HOME", "").strip()
    cache_root = Path(configured) if configured else Path.home() / ".cache"
    return cache_root / "rmapi" / "tree.cache"


def clear_rmapi_tree_cache() -> bool:
    cache_path = rmapi_tree_cache_path()
    try:
        cache_path.unlink()
        return True
    except FileNotFoundError:
        return False
    except OSError as exc:
        raise SyncError("Der lokale reMarkable-Cloud-Baum konnte nicht sicher erneuert werden.") from exc


def run_remarquee(
    args: list[str], *, check: bool = True, timeout: float | None = None
) -> subprocess.CompletedProcess[str]:
    """Run one private remarquee command with pacing and bounded recovery."""
    global _last_remarquee_started

    read_only = args[:2] in (["cloud", "stat"], ["cloud", "refresh"], ["cloud", "ls"])
    # A cold tree can take several minutes. Share one budget across retries,
    # rather than killing each partial mirror and starting it over repeatedly.
    read_budget = timeout if timeout is not None else REMARKABLE_READ_TIMEOUT_SECONDS
    deadline = time.monotonic() + read_budget if read_only else None
    generation_attempt = 0
    protocol_attempt = 0
    transient_attempt = 0
    while True:
        if REMARQUEE_MIN_INTERVAL_SECONDS > 0 and _last_remarquee_started:
            elapsed = time.monotonic() - _last_remarquee_started
            remaining = REMARQUEE_MIN_INTERVAL_SECONDS - elapsed
            if remaining > 0:
                time.sleep(remaining)

        _last_remarquee_started = time.monotonic()
        command_timeout = timeout
        if deadline is not None:
            command_timeout = deadline - _last_remarquee_started
            if command_timeout <= 0:
                raise RemarqueeCommandError("timeout", safe_remarquee_failure_message("timeout"), diagnostic_code="RM-READ-TIMEOUT")
        # Debug output is captured privately so response bodies can be classified;
        # it is never copied into logs, dashboard messages, or diagnostics.
        result = run(["remarquee", "--log-level", "debug", *args], check=False, timeout=command_timeout)
        if result.returncode != 0 and is_rate_limited_result(result):
            raise RateLimitError("reMarkable-Cloud meldet HTTP 429 (zu viele Anfragen).")
        if not check or result.returncode == 0:
            return result
        if read_only and getattr(result, "timed_out", False):
            raise RemarqueeCommandError("timeout", safe_remarquee_failure_message("timeout"), diagnostic_code="RM-READ-TIMEOUT")

        reason = classify_remarquee_failure(result)
        if reason == "generation" and generation_attempt < len(REMARQUEE_GENERATION_RETRY_DELAYS_SECONDS):
            delay = REMARQUEE_GENERATION_RETRY_DELAYS_SECONDS[generation_attempt]
            generation_attempt += 1
            clear_rmapi_tree_cache()
            log(f"reMarkable-Cloud wurde parallel geändert; sicherer neuer Versuch in {delay} Sekunden.")
            time.sleep(delay)
            continue
        if reason == "protocol" and protocol_attempt < len(REMARQUEE_PROTOCOL_RETRY_DELAYS_SECONDS):
            delay = REMARQUEE_PROTOCOL_RETRY_DELAYS_SECONDS[protocol_attempt]
            protocol_attempt += 1
            clear_rmapi_tree_cache()
            log(f"Lokaler reMarkable-Cloud-Baum wird sicher erneuert; neuer Versuch in {delay} Sekunden.")
            time.sleep(delay)
            continue
        if read_only and reason == "network" and transient_attempt < len(REMARQUEE_TRANSIENT_RETRY_DELAYS_SECONDS):
            delay = REMARQUEE_TRANSIENT_RETRY_DELAYS_SECONDS[transient_attempt]
            transient_attempt += 1
            log(f"reMarkable-Cloud war vorübergehend nicht erreichbar; sicherer neuer Versuch in {delay} Sekunden.")
            time.sleep(delay)
            continue
        raise RemarqueeCommandError(reason, safe_remarquee_failure_message(reason))


def load_json(path: Path, default: Any) -> Any:
    if not path.exists():
        return default
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return default


def read_json_strict(path: Path, label: str) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        raise
    except (OSError, json.JSONDecodeError) as exc:
        raise SyncError(f"{label} ist beschädigt oder nicht lesbar; Synchronisation wurde sicher angehalten.") from exc


def atomic_write_bytes(path: Path, value: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary: Path | None = None
    try:
        with tempfile.NamedTemporaryFile("wb", dir=path.parent, delete=False) as handle:
            temporary = Path(handle.name)
            os.chmod(temporary, 0o600)
            handle.write(value)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
    finally:
        if temporary is not None and temporary.exists():
            temporary.unlink(missing_ok=True)


def atomic_write_json(path: Path, value: Any) -> None:
    raw = (json.dumps(value, indent=2, ensure_ascii=False, sort_keys=True) + "\n").encode("utf-8")
    atomic_write_bytes(path, raw)


def validate_control_target(path: str) -> str:
    if not CONTROL_URL or not SYNC_API_TOKEN:
        raise SyncError("CONTROL_URL oder SYNC_API_TOKEN fehlt.")
    parsed = urllib.parse.urlsplit(CONTROL_URL)
    if (
        parsed.scheme.lower() != "https"
        or not parsed.hostname
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
        or parsed.path not in {"", "/"}
    ):
        raise SyncError("CONTROL_URL ist ungültig oder nicht sicher.")
    if not path.startswith("/api/") or "//" in path or ".." in path or any(ch in path for ch in "\r\n\x00"):
        raise SyncError("Ungültiger Dashboard-API-Pfad.")
    return CONTROL_URL + path


def control_request(path: str, *, method: str = "GET", value: Any = None) -> Any:
    target = validate_control_target(path)
    data = None if value is None else json.dumps(value, separators=(",", ":")).encode("utf-8")
    request = urllib.request.Request(
        target,
        data=data,
        method=method,
        headers={
            "Authorization": f"Bearer {SYNC_API_TOKEN}",
            "Content-Type": "application/json",
            "User-Agent": "iserv-remarkable-sync/4",
        },
    )
    opener = urllib.request.build_opener(NoRedirectHandler())
    try:
        with opener.open(request, timeout=20) as response:
            payload = response.read(MAX_CONTROL_RESPONSE + 1)
    except urllib.error.HTTPError as exc:
        raise SyncError(f"Dashboard-API meldet HTTP {exc.code}.") from exc
    except urllib.error.URLError as exc:
        raise SyncError("Dashboard-API ist nicht erreichbar.") from exc
    if len(payload) > MAX_CONTROL_RESPONSE:
        raise SyncError("Dashboard-API-Antwort ist unerwartet groß.")
    if not payload:
        return None
    try:
        return json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise SyncError("Dashboard-API hat eine ungültige Antwort geliefert.") from exc


def load_sync_config() -> dict[str, Any]:
    if CONTROL_URL or SYNC_API_TOKEN:
        # New settings may take time to become visible from the Fly region.
        for delay in (0, 3, 8, 15, 30):
            if delay:
                time.sleep(delay)
            try:
                config = control_request("/api/sync-config")
            except SyncError as exc:
                cause = exc.__cause__
                if not isinstance(cause, urllib.error.HTTPError) or cause.code != 503 or delay == 30:
                    raise
            else:
                log("Konfiguration aus dem Web-Dashboard geladen.")
                return config
    log("Noch kein Web-Dashboard verbunden; verwende lokale Testkonfiguration.")
    return load_json(CONFIG_PATH, None)


def report_status(value: dict[str, Any]) -> None:
    if not CONTROL_URL or not SYNC_API_TOKEN:
        return
    for delay in (0, 2, 5):
        if delay:
            time.sleep(delay)
        try:
            control_request("/api/report", method="POST", value=value)
        except Exception as exc:
            cause = exc.__cause__
            if isinstance(cause, urllib.error.HTTPError) and cause.code in {429, 503}:
                continue
            break
        log("Ergebnis an das Web-Dashboard gemeldet.")
        return
    log("WARNUNG: Statusmeldung an das Web-Dashboard fehlgeschlagen.")


def install_rmapi_config() -> None:
    """Install a changed secret seed once without clobbering refreshed rmapi state.

    The persistent rmapi file may be updated by the cloud client itself. A hash
    marker records which Fly secret seed produced it. Credential repair changes
    the seed, so the next run replaces the file exactly once; later runs preserve
    any refreshed token material written by rmapi/remarquee.
    """
    force_replace = os.environ.get("RMAPI_CONFIG_REPLACE", "false").lower() in {"1", "true", "yes", "on"}
    encoded = os.environ.get("RMAPI_CONFIG_B64", "").strip()
    if not encoded:
        if not RMAPI_CONFIG_PATH.exists():
            raise SyncError("RMAPI_CONFIG_B64 fehlt und es gibt keine gespeicherte rmapi-Konfiguration.")
        return
    try:
        decoded = base64.b64decode(encoded, validate=True)
    except Exception as exc:
        raise SyncError("RMAPI_CONFIG_B64 ist kein gültiges Base64.") from exc
    if not decoded or len(decoded) > MAX_RMAPI_CONFIG_BYTES:
        raise SyncError("RMAPI_CONFIG_B64 hat eine ungültige Größe.")

    seed_digest = hashlib.sha256(decoded).hexdigest()
    previous_digest = ""
    try:
        previous_digest = RMAPI_CONFIG_SEED_PATH.read_text(encoding="ascii").strip()
    except FileNotFoundError:
        pass
    except OSError as exc:
        raise SyncError("reMarkable-Zugangsdatenstatus konnte nicht sicher gelesen werden.") from exc

    if RMAPI_CONFIG_PATH.exists() and previous_digest == seed_digest and not force_replace:
        return

    atomic_write_bytes(RMAPI_CONFIG_PATH, decoded)
    atomic_write_bytes(RMAPI_CONFIG_SEED_PATH, (seed_digest + "\n").encode("ascii"))


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def safe_label(value: str) -> str:
    cleaned = "".join(char if char.isalnum() or char in "-_. " else "_" for char in value).strip()
    return cleaned or "Quelle"


def source_tag(source: dict[str, Any]) -> str:
    identity = f"{source.get('name', '')}\0{source.get('iserv_path', '')}\0{source.get('remarkable_dir', '')}".encode("utf-8")
    return hashlib.sha256(identity).hexdigest()[:10]


def empty_manifest() -> dict[str, Any]:
    return {"version": 2, "files": {}}


def valid_manifest_entry(key: Any, value: Any) -> bool:
    if not isinstance(key, str) or not key.startswith("v2:") or len(key) != 67:
        return False
    if not isinstance(value, dict):
        return False
    digest = value.get("sha256")
    return isinstance(digest, str) and len(digest) == 64 and all(ch in "0123456789abcdef" for ch in digest.lower())


def load_manifest(path: Path | None = None) -> dict[str, Any]:
    """Load manifest state without ever turning corrupt v2 state into an empty manifest.

    A valid v1 manifest is intentionally migrated once because v2 changed target
    identity. Malformed or unknown state fails closed to prevent duplicate uploads.
    """
    path = path or MANIFEST_PATH
    if not path.exists():
        return empty_manifest()
    raw = read_json_strict(path, "Synchronisationsmanifest")
    if isinstance(raw, dict) and raw.get("version") == 2 and isinstance(raw.get("files"), dict):
        if not all(valid_manifest_entry(key, value) for key, value in raw["files"].items()):
            raise SyncError("Synchronisationsmanifest enthält ungültige Einträge; Synchronisation wurde sicher angehalten.")
        return raw
    if isinstance(raw, dict) and raw.get("version") == 1 and isinstance(raw.get("files"), dict):
        backup_path = path.with_name("manifest-pre-v2.json")
        if not backup_path.exists():
            shutil.copy2(path, backup_path)
            os.chmod(backup_path, 0o600)
            log("Altes Manifest wurde vor der v2-Migration lokal gesichert.")
        log("Manifest v1 erkannt; starte die vorgesehene einmalige Migration auf v2.")
        return empty_manifest()
    raise SyncError("Synchronisationsmanifest hat ein unbekanntes Format; Synchronisation wurde sicher angehalten.")


def load_state(path: Path | None = None) -> dict[str, Any]:
    path = path or STATE_PATH
    if not path.exists():
        return {"version": 1, "cooldown_until": None}
    raw = read_json_strict(path, "Sync-Zustand")
    if not isinstance(raw, dict) or raw.get("version", 1) != 1:
        raise SyncError("Sync-Zustand hat ein unbekanntes Format; Synchronisation wurde sicher angehalten.")
    cooldown = raw.get("cooldown_until")
    if cooldown is not None and parse_iso_datetime(cooldown) is None:
        raise SyncError("Sync-Zustand enthält einen ungültigen Cooldown; Synchronisation wurde sicher angehalten.")
    raw["version"] = 1
    return raw


def manifest_key(source: dict[str, Any], relative: Path) -> str:
    identity = {
        "source_name": str(source["name"]),
        "iserv_path": str(source["iserv_path"]).strip("/"),
        "remarkable_dir": str(source["remarkable_dir"]).rstrip("/") or "/",
        "relative_path": relative.as_posix(),
    }
    encoded = json.dumps(identity, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return "v2:" + hashlib.sha256(encoded).hexdigest()


def target_directory(source: dict[str, Any], relative: Path) -> str:
    base = PurePosixPath(str(source["remarkable_dir"]).rstrip("/") or "/")
    parent = PurePosixPath(relative.parent.as_posix())
    return str(base / parent)


def parse_iso_datetime(value: Any) -> datetime | None:
    if not isinstance(value, str) or not value.strip():
        return None
    try:
        parsed = datetime.fromisoformat(value)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return parsed.astimezone()
    return parsed


def cooldown_remaining_seconds(state: dict[str, Any], now: datetime | None = None) -> int:
    now = now or datetime.now().astimezone()
    until = parse_iso_datetime(state.get("cooldown_until"))
    if until is None:
        return 0
    return max(0, int((until - now).total_seconds()))


def set_rate_limit_cooldown(state: dict[str, Any], now: datetime | None = None) -> str:
    now = now or datetime.now().astimezone()
    until = now + timedelta(seconds=RATE_LIMIT_COOLDOWN_SECONDS)
    state["version"] = 1
    state["last_rate_limit_at"] = now.isoformat(timespec="seconds")
    state["cooldown_until"] = until.isoformat(timespec="seconds")
    atomic_write_json(STATE_PATH, state)
    return state["cooldown_until"]


def clear_expired_cooldown(state: dict[str, Any]) -> None:
    if state.get("cooldown_until") and cooldown_remaining_seconds(state) == 0:
        state["cooldown_until"] = None
        atomic_write_json(STATE_PATH, state)


def path_is_safe(value: str, *, allow_root: bool) -> bool:
    if not value or len(value) > 500 or any(ch in value for ch in "\\\x00\r\n") or "//" in value:
        return False
    if allow_root and value == "/":
        return True
    parts = value.strip("/").split("/")
    return bool(parts) and all(part not in {"", ".", ".."} for part in parts)


def validate_sync_config(config: Any) -> dict[str, Any]:
    if not isinstance(config, dict):
        raise SyncError("Ungültige oder leere Synchronisationskonfiguration.")
    dry_run = config.get("dry_run", DEFAULT_DRY_RUN)
    if not isinstance(dry_run, bool):
        raise SyncError("dry_run muss ein boolescher Wert sein.")
    lookback = config.get("lookback_days", 30)
    max_file = config.get("max_file_mb", 95)
    if isinstance(lookback, bool) or not isinstance(lookback, int) or not 1 <= lookback <= 3650:
        raise SyncError("lookback_days muss zwischen 1 und 3650 liegen.")
    if isinstance(max_file, bool) or not isinstance(max_file, int) or not 1 <= max_file <= 95:
        raise SyncError("max_file_mb muss zwischen 1 und 95 liegen.")
    raw_extensions = config.get("extensions", ["pdf", "epub"])
    if not isinstance(raw_extensions, list) or not raw_extensions:
        raise SyncError("Mindestens ein unterstützter Dateityp ist erforderlich.")
    extensions: list[str] = []
    for extension in raw_extensions:
        if not isinstance(extension, str) or extension not in {"pdf", "epub"}:
            raise SyncError("Unbekannter Dateityp in der Synchronisationskonfiguration.")
        if extension not in extensions:
            extensions.append(extension)
    raw_sources = config.get("sources")
    if not isinstance(raw_sources, list) or not 1 <= len(raw_sources) <= 20:
        raise SyncError("Es sind 1 bis 20 Ordnerzuordnungen erforderlich.")
    names: set[str] = set()
    sources: list[dict[str, str]] = []
    for source in raw_sources:
        if not isinstance(source, dict):
            raise SyncError("Ungültige Ordnerzuordnung.")
        name = source.get("name")
        iserv_path = source.get("iserv_path")
        remarkable_dir = source.get("remarkable_dir")
        if not all(isinstance(value, str) for value in (name, iserv_path, remarkable_dir)):
            raise SyncError("Ordnerzuordnung enthält ungültige Werte.")
        name = name.strip()
        iserv_path = iserv_path.strip().strip("/")
        remarkable_dir = remarkable_dir.strip()
        if not name or len(name) > 100 or name.casefold() in names:
            raise SyncError("Zuordnungsnamen müssen vorhanden und eindeutig sein.")
        if not path_is_safe(iserv_path, allow_root=False):
            raise SyncError("Unsicherer IServ-Ordnerpfad in der Synchronisationskonfiguration.")
        if not remarkable_dir.startswith("/") or not path_is_safe(remarkable_dir, allow_root=True):
            raise SyncError("Unsicherer reMarkable-Zielpfad in der Synchronisationskonfiguration.")
        names.add(name.casefold())
        sources.append({"name": name, "iserv_path": iserv_path, "remarkable_dir": remarkable_dir.rstrip("/") or "/"})
    return {
        "dry_run": dry_run,
        "lookback_days": lookback,
        "max_file_mb": max_file,
        "extensions": extensions,
        "sources": sources,
    }


def is_missing_remote_result(result: subprocess.CompletedProcess[str]) -> bool:
    if result.returncode == 0:
        return False
    output = ANSI_ESCAPE_RE.sub("", f"{result.stdout or ''}\n{result.stderr or ''}").lower()
    # rmapi's local tree lookup uses the unpunctuated spelling "doesnt".
    # Match the complete diagnostic line, not arbitrary provider response text.
    missing_entry = re.search(r"(?m)^\s*(?:error:\s*)?entry '[^\r\n]+' doesnt exist\s*$", output)
    if missing_entry:
        return True
    legacy_missing = re.search(
        r"(?m)^\s*(?:error:\s*)?(?:file doesn't exist|file does not exist|entry not found)\s*$",
        output,
    )
    return legacy_missing is not None


def ensure_remote_directory(remote_dir: str, ensured_dirs: set[str]) -> None:
    current = ""
    for part in PurePosixPath(remote_dir).parts:
        if part in {"/", ""}:
            continue
        current += "/" + part
        if current in ensured_dirs:
            continue

        probe = run_remarquee(["cloud", "stat", current, "--non-interactive"], check=False)
        if probe.returncode == 0:
            if "is_dir=false" in (probe.stdout or "").splitlines():
                raise remarkable_error("target", "RM-TARGET-NOT-DIRECTORY")
            ensured_dirs.add(current)
            continue
        if not is_missing_remote_result(probe):
            reason = classify_remarquee_failure(probe)
            raise remarkable_error(reason, "RM-TARGET-LOOKUP")

        log("Fehlender reMarkable-Zielordner wird angelegt.")
        try:
            run_remarquee(["cloud", "mkdir", current, "--non-interactive"])
        except RemarqueeCommandError as exc:
            if exc.reason != "conflict":
                raise remarkable_error(exc.reason, "RM-TARGET-CREATE") from exc
            # Another client may have created it after the missing lookup.
            # Accept that race only after confirming it is a directory.
            confirmed = run_remarquee(["cloud", "stat", current, "--non-interactive"], check=False)
            if confirmed.returncode != 0 or "is_dir=true" not in (confirmed.stdout or "").splitlines():
                raise remarkable_error("target", "RM-TARGET-CREATE-CONFLICT") from exc
        ensured_dirs.add(current)


def versioned_copy(path: Path, work_dir: Path, digest: str) -> Path:
    stamp = datetime.fromtimestamp(path.stat().st_mtime).astimezone().strftime("%Y-%m-%d %H-%M-%S")
    content_tag = digest[:8]
    target = work_dir / f"{path.stem} (aktualisiert {stamp} {content_tag}){path.suffix.lower()}"
    shutil.copy2(path, target)
    return target


def download_source(source: dict[str, Any], config: dict[str, Any]) -> Path:
    identity = f"{source['name']}\0{source['iserv_path']}".encode("utf-8")
    source_name = f"{safe_label(source['name'])}-{hashlib.sha256(identity).hexdigest()[:12]}"
    local_dir = DATA_DIR / "cache" / source_name
    local_dir.mkdir(parents=True, exist_ok=True)

    command = [
        "rclone",
        "sync",
        f"iserv:{source['iserv_path'].strip('/')}",
        str(local_dir),
    ]
    patterns = {
        "pdf": "*.[pP][dD][fF]",
        "epub": "*.[eE][pP][uU][bB]",
    }
    for extension in config["extensions"]:
        command.extend(["--include", patterns[extension]])
    command.extend([
        "--max-age",
        f"{config['lookback_days']}d",
        "--max-size",
        f"{config['max_file_mb']}M",
        "--delete-excluded",
        "--create-empty-src-dirs",
        "--log-level",
        "NOTICE",
    ])
    log(f"Prüfe IServ-Quelle {source_tag(source)}.")
    run_rclone(command)
    return local_dir


def eligible_files(local_dir: Path, max_file_mb: int, extensions: set[str]) -> list[Path]:
    limit = max_file_mb * 1024 * 1024
    return sorted(
        path
        for path in local_dir.rglob("*")
        if path.is_file() and path.suffix.lower().lstrip(".") in extensions and path.stat().st_size <= limit
    )


def rmapi_token(name: str, *, required: bool = True) -> str:
    try:
        raw = RMAPI_CONFIG_PATH.read_text(encoding="utf-8")
    except OSError as exc:
        raise RemarqueeCommandError("auth", safe_remarquee_failure_message("auth")) from exc
    if len(raw.encode("utf-8")) > MAX_RMAPI_CONFIG_BYTES:
        raise RemarqueeCommandError("auth", safe_remarquee_failure_message("auth"))
    for line in raw.splitlines():
        key, separator, value = line.partition(":")
        if separator and key.strip().lower() == name:
            token = value.strip()
            if len(token) >= 2 and token[0] == token[-1] and token[0] in {"'", '"'}:
                token = token[1:-1]
            if token and len(token) <= 64 * 1024 and not any(char.isspace() for char in token):
                return token
    if required:
        raise RemarqueeCommandError("auth", safe_remarquee_failure_message("auth"))
    return ""


def jwt_expiry(token: str) -> int | None:
    """Read the bounded JWT expiry without trusting it for authentication."""
    if not token or len(token) > 64 * 1024:
        return None
    parts = token.split(".")
    if len(parts) != 3 or not JWT_SEGMENT_RE.fullmatch(parts[1]):
        return None
    encoded = parts[1] + "=" * (-len(parts[1]) % 4)
    try:
        raw = base64.urlsafe_b64decode(encoded.encode("ascii"))
        if len(raw) > MAX_REMARKABLE_AUTH_RESPONSE:
            return None
        value = json.loads(raw.decode("utf-8"))
    except (ValueError, UnicodeError, json.JSONDecodeError):
        return None
    expiry = value.get("exp") if isinstance(value, dict) else None
    return expiry if isinstance(expiry, int) and not isinstance(expiry, bool) else None


def valid_cached_user_token(token: str, *, now: float | None = None) -> bool:
    expiry = jwt_expiry(token)
    if expiry is None:
        return False
    now = time.time() if now is None else now
    return expiry - REMARKABLE_SESSION_SKEW_SECONDS > now


def remarkable_user_token(*, force_refresh: bool = False) -> str:
    global _remarkable_user_token
    if not force_refresh:
        if valid_cached_user_token(_remarkable_user_token):
            return _remarkable_user_token
        persisted = rmapi_token("usertoken", required=False)
        if valid_cached_user_token(persisted):
            _remarkable_user_token = persisted
            return persisted

    device_token = rmapi_token("devicetoken")
    opener = urllib.request.build_opener(NoRedirectHandler())
    fallback_statuses = {301, 302, 307, 308, 404, 405, 408, 425, 500, 502, 503, 504}
    for index, auth_url in enumerate(REMARKABLE_AUTH_URLS):
        request = urllib.request.Request(
            auth_url,
            data=b"",
            method="POST",
            headers={
                "Authorization": f"Bearer {device_token}",
                "Content-Type": "application/json",
                "User-Agent": "iserv-remarkable-sync/6",
            },
        )
        try:
            with opener.open(request, timeout=30) as response:
                payload = response.read(MAX_REMARKABLE_AUTH_RESPONSE + 1)
        except urllib.error.HTTPError as exc:
            if exc.code == 429:
                raise RateLimitError("reMarkable-Cloud meldet HTTP 429 (zu viele Anfragen).") from exc
            if exc.code in fallback_statuses and index + 1 < len(REMARKABLE_AUTH_URLS):
                continue
            reason = "auth" if exc.code in {401, 403} else "network" if exc.code in {408, 425, 500, 502, 503, 504} else "provider"
            raise remarkable_error(reason, f"RM-SESSION-HTTP-{exc.code}") from exc
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            if index + 1 < len(REMARKABLE_AUTH_URLS):
                continue
            raise remarkable_error("network", "RM-SESSION-NETWORK") from exc
        if len(payload) > MAX_REMARKABLE_AUTH_RESPONSE:
            if index + 1 < len(REMARKABLE_AUTH_URLS):
                continue
            raise remarkable_error("provider", "RM-SESSION-OVERSIZE")
        try:
            token = payload.decode("ascii").strip()
        except UnicodeDecodeError as exc:
            if index + 1 < len(REMARKABLE_AUTH_URLS):
                continue
            raise remarkable_error("provider", "RM-SESSION-ENCODING") from exc
        if not valid_cached_user_token(token):
            if index + 1 < len(REMARKABLE_AUTH_URLS):
                continue
            raise remarkable_error("provider", "RM-SESSION-TOKEN")
        _remarkable_user_token = token
        return token
    raise AssertionError("unreachable")


def remote_directory_id(remote_dir: str) -> str:
    result = run_remarquee(["cloud", "stat", remote_dir, "--non-interactive"])
    values: dict[str, str] = {}
    for line in (result.stdout or "").splitlines():
        key, separator, value = line.partition("=")
        if separator and key in {"id", "is_dir"}:
            values[key] = value.strip()
    directory_id = values.get("id", "")
    if values.get("is_dir") != "true" or (directory_id and not REMARKABLE_ID_RE.fullmatch(directory_id)):
        raise RemarqueeCommandError("target", safe_remarquee_failure_message("target"))
    return directory_id


def remote_document_path(remote_dir: str, local: Path) -> str:
    base = remote_dir.rstrip("/")
    return f"{base}/{local.stem}" if base else f"/{local.stem}"


def remote_document_exists(remote_dir: str, local: Path) -> bool:
    clear_rmapi_tree_cache()
    result = run_remarquee([
        "cloud",
        "stat",
        remote_document_path(remote_dir, local),
        "--non-interactive",
    ], check=False)
    if result.returncode == 0:
        return True
    if is_missing_remote_result(result):
        return False
    reason = classify_remarquee_failure(result)
    raise RemarqueeCommandError(reason, safe_remarquee_failure_message(reason))


def validated_remarkable_upload_url(value: str) -> tuple[str, int | None, str]:
    parsed = urllib.parse.urlsplit(value)
    hostname = (parsed.hostname or "").lower().rstrip(".")
    if (
        parsed.scheme != "https"
        or not hostname
        or parsed.username is not None
        or parsed.password is not None
        or parsed.fragment
        or not any(hostname.endswith(suffix) for suffix in REMARKABLE_UPLOAD_HOST_SUFFIXES)
    ):
        raise remarkable_error("provider", "RM-UPLOAD-REDIRECT")
    try:
        port = parsed.port
    except ValueError as exc:
        raise remarkable_error("provider", "RM-UPLOAD-REDIRECT") from exc
    if port not in {None, 443}:
        raise remarkable_error("provider", "RM-UPLOAD-REDIRECT")
    target = parsed.path or "/"
    if parsed.query:
        target += "?" + parsed.query
    return hostname, port, target


def native_upload_once(local: Path, parent_id: str, token: str) -> None:
    mime = "application/pdf" if local.suffix.lower() == ".pdf" else "application/epub+zip"
    metadata = base64.b64encode(json.dumps(
        {"file_name": local.stem, "parent": parent_id},
        ensure_ascii=False,
        separators=(",", ":"),
    ).encode("utf-8")).decode("ascii")
    upload_url = f"https://{REMARKABLE_UPLOAD_HOST}{REMARKABLE_UPLOAD_PATH}"
    for redirect_count in range(MAX_REMARKABLE_UPLOAD_REDIRECTS + 1):
        host, port, target = validated_remarkable_upload_url(upload_url)
        connection = http.client.HTTPSConnection(host, port=port, timeout=90)
        try:
            connection.putrequest("POST", target, skip_accept_encoding=True)
            connection.putheader("Authorization", f"Bearer {token}")
            connection.putheader("Content-Type", mime)
            connection.putheader("Content-Length", str(local.stat().st_size))
            connection.putheader("rm-meta", metadata)
            connection.putheader("rm-source", "RoR-Browser")
            connection.putheader("User-Agent", "iserv-remarkable-sync/6")
            connection.endheaders()
            with local.open("rb") as handle:
                for block in iter(lambda: handle.read(1024 * 1024), b""):
                    connection.send(block)
            response = connection.getresponse()
            payload = response.read(MAX_REMARKABLE_UPLOAD_RESPONSE + 1)
            status = response.status
            location = response.getheader("Location", "")
        except (OSError, TimeoutError, http.client.HTTPException) as exc:
            raise remarkable_error("network", "RM-UPLOAD-NETWORK", ambiguous_write=True) from exc
        finally:
            connection.close()

        if status in {307, 308}:
            if redirect_count >= MAX_REMARKABLE_UPLOAD_REDIRECTS or not location:
                raise remarkable_error("provider", "RM-UPLOAD-REDIRECT", ambiguous_write=True)
            upload_url = urllib.parse.urljoin(upload_url, location)
            validated_remarkable_upload_url(upload_url)
            continue
        break
    else:
        raise AssertionError("unreachable")

    if status == 429:
        raise RateLimitError("reMarkable-Cloud meldet HTTP 429 (zu viele Anfragen).")
    if status in {401, 403}:
        raise remarkable_error("auth", f"RM-UPLOAD-HTTP-{status}")
    if status in {408, 425, 500, 502, 503, 504}:
        raise remarkable_error("network", f"RM-UPLOAD-HTTP-{status}", ambiguous_write=True)
    if status in {413, 415, 422}:
        raise remarkable_error("document", f"RM-UPLOAD-HTTP-{status}")
    if not 200 <= status < 300 or len(payload) > MAX_REMARKABLE_UPLOAD_RESPONSE:
        ambiguous = 200 <= status < 300
        code = "RM-UPLOAD-OVERSIZE" if ambiguous else f"RM-UPLOAD-HTTP-{status}"
        raise remarkable_error("provider", code, ambiguous_write=ambiguous)
    try:
        value = json.loads(payload.decode("utf-8"))
        document_id = value["docID"]
        document_hash = value["hash"]
    except (UnicodeDecodeError, json.JSONDecodeError, KeyError, TypeError) as exc:
        raise remarkable_error("provider", "RM-UPLOAD-RESPONSE", ambiguous_write=True) from exc
    if (
        not isinstance(document_id, str)
        or not document_id
        or len(document_id) > 256
        or any(ord(char) < 0x20 or ord(char) == 0x7f for char in document_id)
    ):
        raise remarkable_error("provider", "RM-UPLOAD-RESPONSE", ambiguous_write=True)
    if not isinstance(document_hash, str) or not REMARKABLE_HASH_RE.fullmatch(document_hash):
        raise remarkable_error("provider", "RM-UPLOAD-RESPONSE", ambiguous_write=True)


def native_upload_file(local: Path, remote_dir: str) -> None:
    if remote_document_exists(remote_dir, local):
        raise RemarqueeCommandError("conflict", safe_remarquee_failure_message("conflict"))
    parent_id = remote_directory_id(remote_dir)
    token = remarkable_user_token()
    auth_refreshed = False
    for attempt in range(len(REMARQUEE_TRANSIENT_RETRY_DELAYS_SECONDS) + 1):
        try:
            native_upload_once(local, parent_id, token)
            return
        except RemarqueeCommandError as exc:
            if exc.reason == "auth" and not auth_refreshed:
                token = remarkable_user_token(force_refresh=True)
                auth_refreshed = True
                continue
            if exc.ambiguous_write:
                for delay in (0, *REMARQUEE_TRANSIENT_RETRY_DELAYS_SECONDS):
                    if delay:
                        log(f"Unklare Upload-Antwort; prüfe den Cloud-Stand erneut in {delay} Sekunden.")
                        time.sleep(delay)
                    if remote_document_exists(remote_dir, local):
                        log(f"Nativer reMarkable-Upload wurde trotz unklarer Antwort bestätigt ({exc.diagnostic_code}).")
                        return
            if exc.reason != "network" or attempt >= len(REMARQUEE_TRANSIENT_RETRY_DELAYS_SECONDS):
                raise
            delay = REMARQUEE_TRANSIENT_RETRY_DELAYS_SECONDS[attempt]
            log(f"Nativer reMarkable-Upload wurde unterbrochen; prüfe den Cloud-Stand in {delay} Sekunden.")
            time.sleep(delay)
            if remote_document_exists(remote_dir, local):
                return
            log("Der Upload wurde nachweislich nicht gespeichert; starte einen sicheren neuen Versuch.")
    raise AssertionError("unreachable")


def upload_file(local: Path, remote_dir: str) -> None:
    try:
        run_remarquee([
            "cloud",
            "put",
            str(local),
            remote_dir,
            "--non-interactive",
        ])
    except RemarqueeCommandError as exc:
        if exc.reason not in {"protocol", "provider"}:
            raise
        log("Low-Level-Upload wurde abgelehnt; wechsle sicher auf den nativen reMarkable-Dokumentimport.")
        native_upload_file(local, remote_dir)


def upload_candidate(
    candidate: UploadCandidate,
    work_dir: Path,
    ensured_dirs: set[str],
    allow_conflict_retry: bool,
) -> int:
    ensure_remote_directory(candidate.target_dir, ensured_dirs)
    upload_path = candidate.path if candidate.previous is None else versioned_copy(candidate.path, work_dir, candidate.digest)
    try:
        upload_file(upload_path, candidate.target_dir)
    except RemarqueeCommandError as exc:
        if exc.reason != "conflict" or candidate.previous is not None:
            raise
        if not allow_conflict_retry:
            raise UploadAttemptLimit("Upload-Limit vor sicherer Konfliktkopie erreicht.") from None
        # A fresh local manifest may point at a folder populated by an older
        # installation. Preserve that remote document and upload a uniquely
        # content-addressed copy instead of ever using --force.
        log("Gleichnamiges reMarkable-Dokument bleibt erhalten; lade eine sichere versionierte Kopie hoch.")
        upload_file(versioned_copy(candidate.path, work_dir, candidate.digest), candidate.target_dir)
        return 2
    return 1


def make_stats() -> dict[str, int]:
    return {
        "checked": 0,
        "uploaded": 0,
        "unchanged": 0,
        "deferred": 0,
        "failed": 0,
        "would_upload": 0,
    }


def collect_candidates(
    config: dict[str, Any], manifest: dict[str, Any], max_file_mb: int, extensions: set[str]
) -> tuple[list[UploadCandidate], dict[str, int]]:
    candidates: list[UploadCandidate] = []
    stats = make_stats()

    for source in config["sources"]:
        local_dir = download_source(source, config)
        for path in eligible_files(local_dir, max_file_mb, extensions):
            stats["checked"] += 1
            relative = path.relative_to(local_dir)
            key = manifest_key(source, relative)
            digest = sha256(path)
            previous = manifest["files"].get(key)
            if previous and previous.get("sha256") == digest:
                stats["unchanged"] += 1
                continue

            candidates.append(
                UploadCandidate(
                    source=source,
                    path=path,
                    relative=relative,
                    digest=digest,
                    key=key,
                    previous=previous,
                    target_dir=target_directory(source, relative),
                )
            )

    return candidates, stats


def sync() -> SyncRunResult:
    if not CONTROL_URL and not SYNC_API_TOKEN:
        log("Web-Dashboard noch nicht verbunden. Sicherer Einrichtungsstart ohne Synchronisation.")
        return SyncRunResult(make_stats(), True, "success", "setup", "Noch nicht mit dem Dashboard verbunden.")
    if bool(CONTROL_URL) != bool(SYNC_API_TOKEN):
        raise SyncError("Dashboard-Verbindung ist unvollständig; Synchronisation wurde sicher angehalten.")

    config = validate_sync_config(load_sync_config())

    DATA_DIR.mkdir(parents=True, exist_ok=True)
    install_rmapi_config()
    manifest = load_manifest()
    state = load_state()

    dry_run = config["dry_run"]
    remaining = cooldown_remaining_seconds(state)
    if remaining > 0:
        until = str(state.get("cooldown_until"))
        minutes = max(1, (remaining + 59) // 60)
        message = f"reMarkable-Cooldown aktiv. Neuer Versuch frühestens in etwa {minutes} Minute(n)."
        log(message)
        return SyncRunResult(make_stats(), dry_run, "failed" if dry_run else "partial", "cooldown", message, until)
    clear_expired_cooldown(state)

    max_file_mb = config["max_file_mb"]
    extensions = set(config["extensions"])
    try:
        candidates, stats = collect_candidates(config, manifest, max_file_mb, extensions)
    except RcloneCommandError as exc:
        message = str(exc)
        log(f"IServ-Download wurde sicher angehalten ({exc.reason}); Datei- und Ordnernamen werden nicht protokolliert.")
        return SyncRunResult(make_stats(), dry_run, "failed", "iserv_" + exc.reason, message)

    if dry_run:
        log("Prüfe den lesenden Zugriff auf die reMarkable Cloud.")
        try:
            run_remarquee(["cloud", "ls", "/", "--non-interactive"])
        except RateLimitError:
            until = set_rate_limit_cooldown(state)
            message = "reMarkable-Rate-Limit erreicht. Der Test kann nach der Wartezeit wiederholt werden."
            return SyncRunResult(stats, True, "failed", "remarkable_rate_limit", message, until)
        except RemarqueeCommandError as exc:
            return SyncRunResult(stats, True, "failed", "remarkable_" + exc.reason, str(exc))
        stats["would_upload"] = len(candidates)
        log(f"TESTMODUS: {len(candidates)} Datei(en) würden synchronisiert; es wird nichts hochgeladen.")
        message = f"Testmodus: {len(candidates)} Datei(en) würden synchronisiert."
        return SyncRunResult(stats, True, "success", "dry_run", message)

    if not candidates:
        message = "Keine neuen oder geänderten Dateien gefunden."
        log(message)
        return SyncRunResult(stats, False, "success", None, message)

    try:
        log("Aktualisiere reMarkable-Cloud-Dateibaum einmal vor dem Upload.")
        run_remarquee(["cloud", "refresh", "--non-interactive"])
    except RateLimitError:
        stats["deferred"] = len(candidates)
        until = set_rate_limit_cooldown(state)
        message = "reMarkable-Rate-Limit erreicht. Alle ausstehenden Dateien wurden auf einen späteren Lauf verschoben."
        log(message)
        return SyncRunResult(stats, False, "partial", "rate_limit", message, until)
    except RemarqueeCommandError as exc:
        stats["deferred"] = len(candidates)
        return SyncRunResult(stats, False, "failed", "remarkable_" + exc.reason, str(exc))

    ensured_dirs: set[str] = set()
    attempted = 0
    outcome = "success"
    reason: str | None = None
    message: str | None = None
    cooldown_until: str | None = None

    with tempfile.TemporaryDirectory(prefix="rm-upload-") as temp_name:
        upload_work = Path(temp_name)
        for index, candidate in enumerate(candidates):
            if attempted >= MAX_UPLOADS_PER_RUN:
                stats["deferred"] += len(candidates) - index
                outcome = "partial"
                reason = "upload_limit"
                message = (
                    f"Sicherheitslimit von {MAX_UPLOADS_PER_RUN} Upload-Versuchen pro Lauf erreicht. "
                    f"{stats['deferred']} Datei(en) folgen in späteren Läufen."
                )
                log(message)
                break

            candidate_tag = candidate.key[-12:]

            try:
                log(f"Upload {candidate_tag} wird ausgeführt.")
                attempted += upload_candidate(
                    candidate,
                    upload_work,
                    ensured_dirs,
                    allow_conflict_retry=(MAX_UPLOADS_PER_RUN - attempted >= 2),
                )
            except UploadAttemptLimit:
                attempted = MAX_UPLOADS_PER_RUN
                stats["deferred"] += len(candidates) - index
                outcome = "partial"
                reason = "upload_limit"
                message = (
                    f"Sicherheitslimit von {MAX_UPLOADS_PER_RUN} Upload-Versuchen pro Lauf erreicht. "
                    f"{stats['deferred']} Datei(en) folgen in späteren Läufen."
                )
                log(message)
                break
            except RateLimitError:
                stats["deferred"] += len(candidates) - index
                cooldown_until = set_rate_limit_cooldown(state)
                outcome = "partial"
                reason = "rate_limit"
                message = (
                    "reMarkable-Rate-Limit erreicht. Keine weiteren reMarkable-Anfragen in diesem Lauf; "
                    f"{stats['deferred']} Datei(en) bleiben für später."
                )
                log(message)
                break
            except RemarqueeCommandError as exc:
                stats["failed"] += 1
                stats["deferred"] += len(candidates) - index - 1
                outcome = "partial" if stats["uploaded"] > 0 or stats["unchanged"] > 0 else "failed"
                reason = "remarkable_" + exc.reason
                message = str(exc)
                if exc.diagnostic_code != "RM-UNCLASSIFIED":
                    message += f" Diagnosecode {exc.diagnostic_code}."
                log(f"Upload {candidate_tag} ist fehlgeschlagen ({exc.reason}; {exc.diagnostic_code}); weitere Versuche wurden sicher angehalten. Dateiname und Pfad werden nicht protokolliert.")
                break
            except SyncError as exc:
                stats["failed"] += 1
                stats["deferred"] += len(candidates) - index - 1
                outcome = "partial" if stats["uploaded"] > 0 or stats["unchanged"] > 0 else "failed"
                reason = "sync_error"
                message = str(exc)
                log(f"Upload {candidate_tag} ist fehlgeschlagen; weitere Versuche wurden sicher angehalten. Dateiname und Pfad werden nicht protokolliert.")
                break

            manifest["files"][candidate.key] = {
                "sha256": candidate.digest,
                "uploaded_at": datetime.now().astimezone().isoformat(timespec="seconds"),
            }
            atomic_write_json(MANIFEST_PATH, manifest)
            stats["uploaded"] += 1

    if stats["failed"] > 0 and outcome == "success":
        outcome = "partial" if stats["uploaded"] > 0 or stats["unchanged"] > 0 else "failed"
        reason = "errors"
        message = f"{stats['failed']} Datei(en) konnten nicht synchronisiert werden."

    return SyncRunResult(stats, False, outcome, reason, message, cooldown_until)


def main() -> int:
    started = time.monotonic()
    result: SyncRunResult | None = None
    error_message: str | None = None
    return_code = 1

    try:
        result = sync()
        log("Fertig: " + ", ".join(f"{key}={value}" for key, value in result.stats.items()))
        return_code = 0 if result.outcome != "failed" and result.stats["failed"] == 0 else 2
    except Exception as exc:
        log(f"ABBRUCH: {exc}")
        error_message = str(exc)[:500]
        result = SyncRunResult(make_stats(), DEFAULT_DRY_RUN, "failed", "exception", error_message)
        return_code = 1
    finally:
        assert result is not None
        report_status(
            {
                "finished_at": datetime.now().astimezone().isoformat(timespec="seconds"),
                "ok": return_code == 0,
                "dry_run": result.dry_run,
                "duration_seconds": round(time.monotonic() - started, 1),
                "stats": result.stats,
                "outcome": result.outcome,
                "reason": result.reason,
                "message": result.message,
                "cooldown_until": result.cooldown_until,
                "error": error_message,
            }
        )
        remaining = MIN_RUNTIME_SECONDS - (time.monotonic() - started)
        if remaining > 0:
            time.sleep(remaining)
    return return_code


if __name__ == "__main__":
    sys.exit(main())
