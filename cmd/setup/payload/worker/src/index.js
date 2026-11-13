import { DASHBOARD_HTML } from "./dashboard.js";
import { LOGIN_HTML } from "./login.js";
import { DASHBOARD_JS } from "./dashboard_client.js";

const SETTINGS_KEY = "settings-v2";
const STATUS_KEY = "status-v2";
const DAY_NUMBERS = [1, 2, 3, 4, 5, 6, 7];
const SESSION_COOKIE = "iserv_rm_session";
const SESSION_SECONDS = 12 * 60 * 60;
const AUTH_FAILURE_LIMIT = 5;
const AUTH_FAILURE_TTL = 10 * 60;

function cookieValue(request, name) {
  const raw = request.headers.get("Cookie") || "";
  for (const part of raw.split(";")) {
    const [key, ...rest] = part.trim().split("=");
    if (key === name) return rest.join("=");
  }
  return "";
}

function base64Url(bytes) {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

async function hmac(value, secret) {
  const encoder = new TextEncoder();
  const key = await crypto.subtle.importKey("raw", encoder.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return base64Url(new Uint8Array(await crypto.subtle.sign("HMAC", key, encoder.encode(value))));
}

async function createSession(env) {
  const expiry = Math.floor(Date.now() / 1000) + SESSION_SECONDS;
  return `${expiry}.${await hmac(String(expiry), env.ADMIN_PASSWORD)}`;
}

async function validSession(request, env) {
  if (!env.ADMIN_PASSWORD) return false;
  const raw = cookieValue(request, SESSION_COOKIE);
  const [expiryText, signature] = raw.split(".");
  const expiry = Number(expiryText);
  if (!Number.isInteger(expiry) || expiry < Math.floor(Date.now() / 1000) || !signature) return false;
  const expected = await hmac(expiryText, env.ADMIN_PASSWORD);
  return constantTimeEqual(signature, expected);
}

async function adminAuthorizationStatus(request, env) {
  if (!env.ADMIN_PASSWORD) return 401;
  if (await validSession(request, env)) return 200;
  const key = await loginKey(request, env, "basic");
  if (isAdmin(request, env)) {
    await env.CONFIG.delete(key);
    return 200;
  }
  const failures = Number(await env.CONFIG.get(key) || 0);
  if (failures >= AUTH_FAILURE_LIMIT) return 429;
  if ((request.headers.get("Authorization") || "").startsWith("Basic ")) {
    await env.CONFIG.put(key, String(failures + 1), { expirationTtl: AUTH_FAILURE_TTL });
  }
  return 401;
}

function localized(request, de, en) {
  return (request.headers.get("X-UI-Lang") || "").toLowerCase().startsWith("en") ? en : de;
}

async function loginKey(request, env, scope = "browser") {
  const ip = request.headers.get("CF-Connecting-IP") || "unknown";
  return `login-fail:${scope}:${(await hmac(ip, env.ADMIN_PASSWORD)).slice(0, 32)}`;
}

async function handleLogin(request, env) {
  if (request.method !== "POST") return json({ error: "Method" }, 405);
  if (!isSameOrigin(request)) return json({ error: localized(request, "Ungültige Herkunft.", "Invalid origin.") }, 403);
  if (!env.ADMIN_PASSWORD) return json({ error: localized(request, "Dashboard-Passwort fehlt.", "Dashboard password is missing.") }, 500);
  const key = await loginKey(request, env, "browser");
  const failures = Number(await env.CONFIG.get(key) || 0);
  if (failures >= AUTH_FAILURE_LIMIT) return json({ error: localized(request, "Zu viele Fehlversuche. Bitte in einigen Minuten erneut versuchen.", "Too many failed attempts. Try again in a few minutes.") }, 429);
  let body = {};
  try { body = await readSmallJSON(request, 4096); } catch (error) {
    if (error?.message === "request_too_large") return json({ error: localized(request, "Anfrage ist zu groß.", "Request is too large.") }, 413);
    return json({ error: localized(request, "Ungültige Eingabe.", "Invalid input.") }, 400);
  }
  if (!constantTimeEqual(String(body.password || ""), env.ADMIN_PASSWORD)) {
    await env.CONFIG.put(key, String(failures + 1), { expirationTtl: AUTH_FAILURE_TTL });
    return json({ error: localized(request, "Passwort ist nicht korrekt.", "Password is incorrect.") }, 401);
  }
  await env.CONFIG.delete(key);
  const token = await createSession(env);
  return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { ...JSON_HEADERS, "Set-Cookie": `${SESSION_COOKIE}=${token}; Path=/; Max-Age=${SESSION_SECONDS}; HttpOnly; Secure; SameSite=Strict` } });
}

async function handleLogout(request) {
  if (request.method !== "POST") return json({ error: "Method" }, 405);
  if (!isSameOrigin(request)) return json({ error: localized(request, "Ungültige Herkunft.", "Invalid origin.") }, 403);
  return new Response(JSON.stringify({ ok: true }), { status: 200, headers: { ...JSON_HEADERS, "Set-Cookie": `${SESSION_COOKIE}=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Strict` } });
}


function defaultSchedule() {
  const schoolDayWindows = [
    { start: "07:30", end: "12:50" },
    { start: "13:30", end: "15:10" },
  ];
  return Object.fromEntries(
    DAY_NUMBERS.map((day) => [String(day), day <= 5 ? structuredClone(schoolDayWindows) : []]),
  );
}

const DEFAULT_SETTINGS = {
  enabled: false,
  interval_minutes: 10,
  schedule: defaultSchedule(),
  sync: {
    dry_run: true,
    lookback_days: 30,
    max_file_mb: 95,
    extensions: ["pdf", "epub"],
    sources: [
      {
        name: "Mathe",
        iserv_path: "Eigene/Schule/Mathe",
        remarkable_dir: "/IServ/Mathe",
      },
    ],
  },
};

const JSON_HEADERS = {
  "Content-Type": "application/json; charset=utf-8",
  "Cache-Control": "no-store",
  "X-Content-Type-Options": "nosniff",
};

function json(value, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: JSON_HEADERS });
}

function unauthorized() {
  return new Response("Anmeldung erforderlich", { status: 401, headers: JSON_HEADERS });
}

function constantTimeEqual(left, right) {
  if (typeof left !== "string" || typeof right !== "string") return false;
  let mismatch = left.length ^ right.length;
  const length = Math.max(left.length, right.length);
  for (let index = 0; index < length; index += 1) {
    mismatch |= (left.charCodeAt(index) || 0) ^ (right.charCodeAt(index) || 0);
  }
  return mismatch === 0;
}

function base64Utf8(value) {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function isAdmin(request, env) {
  if (!env.ADMIN_PASSWORD) return false;
  const expected = `Basic ${base64Utf8(`admin:${env.ADMIN_PASSWORD}`)}`;
  return constantTimeEqual(request.headers.get("Authorization") || "", expected);
}

function isAgent(request, env) {
  if (!env.SYNC_API_TOKEN) return false;
  return constantTimeEqual(
    request.headers.get("Authorization") || "",
    `Bearer ${env.SYNC_API_TOKEN}`,
  );
}

function isSameOrigin(request) {
  const origin = request.headers.get("Origin");
  return origin === new URL(request.url).origin;
}

function parseClock(value) {
  if (!/^([01]\d|2[0-3]):[0-5]\d$/.test(value)) return null;
  const [hour, minute] = value.split(":").map(Number);
  return hour * 60 + minute;
}

function cleanText(value, maxLength) {
  return typeof value === "string" ? value.trim().slice(0, maxLength) : "";
}

function safeCloudPath(value, { allowRoot = false, requireLeadingSlash = false } = {}) {
  if (typeof value !== "string" || value.length === 0 || value.length > 500) return false;
  if (value.includes("\\") || /[\0\r\n]/.test(value) || value.includes("//")) return false;
  if (requireLeadingSlash && !value.startsWith("/")) return false;
  if (allowRoot && value === "/") return true;
  const parts = value.replace(/^\/+|\/+$/g, "").split("/");
  return parts.length > 0 && parts.every((part) => part !== "" && part !== "." && part !== "..");
}

async function readSmallJSON(request, maxBytes) {
  const length = Number(request.headers.get("Content-Length") || 0);
  if (Number.isFinite(length) && length > maxBytes) throw new Error("request_too_large");
  const raw = await request.arrayBuffer();
  if (raw.byteLength > maxBytes) throw new Error("request_too_large");
  const text = new TextDecoder("utf-8", { fatal: true }).decode(raw);
  return text ? JSON.parse(text) : {};
}

function validateWindows(windowsInput, dayLabel = "Wochentag") {
  const windows = Array.isArray(windowsInput) ? windowsInput : [];
  if (windows.length > 8) {
    throw new Error(`Höchstens acht Zeitfenster pro Wochentag sind erlaubt (${dayLabel}).`);
  }
  return windows.map((window) => {
    const start = cleanText(window?.start, 5);
    const end = cleanText(window?.end, 5);
    const startMinutes = parseClock(start);
    const endMinutes = parseClock(end);
    if (startMinutes === null || endMinutes === null || startMinutes > endMinutes) {
      throw new Error(`Ungültiges Zeitfenster bei ${dayLabel}: ${start}–${end}`);
    }
    return { start, end };
  });
}

function scheduleFromInput(input) {
  const dayLabels = {
    1: "Montag",
    2: "Dienstag",
    3: "Mittwoch",
    4: "Donnerstag",
    5: "Freitag",
    6: "Samstag",
    7: "Sonntag",
  };

  if (input.schedule && typeof input.schedule === "object" && !Array.isArray(input.schedule)) {
    return Object.fromEntries(
      DAY_NUMBERS.map((day) => [
        String(day),
        validateWindows(input.schedule[String(day)], dayLabels[day]),
      ]),
    );
  }

  // Migration from the old schema: selected weekdays all shared one windows list.
  const weekdays = Array.from(
    new Set((Array.isArray(input.weekdays) ? input.weekdays : []).map(Number)),
  ).filter((day) => Number.isInteger(day) && day >= 1 && day <= 7);
  const sharedWindows = validateWindows(input.windows, "bisherigem Wochenplan");
  return Object.fromEntries(
    DAY_NUMBERS.map((day) => [
      String(day),
      weekdays.includes(day) ? structuredClone(sharedWindows) : [],
    ]),
  );
}

function validateSettings(input) {
  if (!input || typeof input !== "object") throw new Error("Konfiguration fehlt.");

  const allowedIntervals = [5, 10, 15, 20, 30, 60];
  const interval = Number(input.interval_minutes);
  if (!allowedIntervals.includes(interval)) throw new Error("Ungültiges Intervall.");

  const schedule = scheduleFromInput(input);

  const syncInput = input.sync && typeof input.sync === "object" ? input.sync : {};
  const lookback = Number(syncInput.lookback_days);
  const maxFile = Number(syncInput.max_file_mb);
  if (!Number.isInteger(lookback) || lookback < 1 || lookback > 3650) {
    throw new Error("Rückblick muss zwischen 1 und 3650 Tagen liegen.");
  }
  if (!Number.isInteger(maxFile) || maxFile < 1 || maxFile > 95) {
    throw new Error("Dateigröße muss zwischen 1 und 95 MB liegen.");
  }

  const extensions = Array.from(
    new Set((Array.isArray(syncInput.extensions) ? syncInput.extensions : []).map(String)),
  ).filter((extension) => ["pdf", "epub"].includes(extension));
  if (extensions.length === 0) throw new Error("Mindestens ein Dateityp ist erforderlich.");

  const sourcesInput = Array.isArray(syncInput.sources) ? syncInput.sources : [];
  if (sourcesInput.length < 1 || sourcesInput.length > 20) {
    throw new Error("Es sind 1 bis 20 Ordnerzuordnungen erforderlich.");
  }
  const names = new Set();
  const sources = sourcesInput.map((source) => {
    const name = cleanText(source.name, 60);
    const iservPath = cleanText(source.iserv_path, 500).replace(/^\/+|\/+$/g, "");
    const remarkableDir = cleanText(source.remarkable_dir, 500).replace(/\/+$/g, "") || "/";
    if (!name || /[\0\r\n]/.test(name) || !safeCloudPath(iservPath) || !safeCloudPath(remarkableDir, { allowRoot: true, requireLeadingSlash: true })) {
      throw new Error("Jede Zuordnung braucht sichere Name-, IServ- und reMarkable-Pfade.");
    }
    const normalized = name.toLocaleLowerCase("de-DE");
    if (names.has(normalized)) throw new Error(`Der Name '${name}' ist doppelt.`);
    names.add(normalized);
    return { name, iserv_path: iservPath, remarkable_dir: remarkableDir };
  });

  return {
    enabled: Boolean(input.enabled),
    interval_minutes: interval,
    schedule,
    sync: {
      dry_run: Boolean(syncInput.dry_run),
      lookback_days: lookback,
      max_file_mb: maxFile,
      extensions,
      sources,
    },
  };
}

async function getSettings(env, fallback = DEFAULT_SETTINGS) {
  const stored = await env.CONFIG.get(SETTINGS_KEY, "json");
  if (!stored) return structuredClone(fallback);

  const normalized = validateSettings(stored);
  // Persist the migrated/normalized shape once, so subsequent scheduler runs no
  // longer depend on legacy weekdays + shared windows fields.
  if (JSON.stringify(stored) !== JSON.stringify(normalized)) {
    await env.CONFIG.put(SETTINGS_KEY, JSON.stringify(normalized));
  }
  return normalized;
}

async function getStatus(env) {
  return (await env.CONFIG.get(STATUS_KEY, "json")) || {
    machine_state: "noch nie gestartet",
    last_start_request: null,
    last_report: null,
  };
}

function berlinParts(date) {
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone: "Europe/Berlin",
    weekday: "short",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  }).formatToParts(date);
  return Object.fromEntries(parts.map(({ type, value }) => [type, value]));
}

function isDue(settings, date) {
  if (!settings.enabled) return false;
  const parts = berlinParts(date);
  const dayNumber = { Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6, Sun: 7 }[
    parts.weekday
  ];
  const schedule = settings.schedule || scheduleFromInput(settings);
  const windows = schedule[String(dayNumber)] || [];
  if (windows.length === 0) return false;

  const now = Number(parts.hour) * 60 + Number(parts.minute);
  return windows.some((window) => {
    const start = parseClock(window.start);
    const end = parseClock(window.end);
    return now >= start && now <= end && (now - start) % settings.interval_minutes === 0;
  });
}

function berlinSlot(date) {
  const parts = berlinParts(date);
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`;
}

function flyAuthorization(token) {
  const value = String(token || "");
  if (value.length > 32 * 1024 || !/^FlyV1 [A-Za-z0-9._~+/=-]{8,4096}(?:,[A-Za-z0-9._~+/=-]{8,4096})*$/.test(value)) {
    const error = new Error("Fly-Steuerberechtigung hat ein ungültiges Format.");
    error.code = "fly_auth_rejected";
    throw error;
  }
  return value;
}

function isFlyMachineTransition(status, body) {
  if (status !== 412) return false;
  let message = "";
  try {
    const parsed = JSON.parse(String(body || ""));
    message = typeof parsed?.error === "string" ? parsed.error : "";
  } catch {
    return false;
  }
  return /^failed_precondition:\s*machine getting replaced,\s*refusing to start$/i.test(message.trim());
}

async function updateStatus(env, patch) {
  const current = await getStatus(env);
  const next = { ...current, ...patch };
  await env.CONFIG.put(STATUS_KEY, JSON.stringify(next));
  return next;
}

async function startMachine(env, trigger) {
  if (!env.FLY_APP || !env.FLY_MACHINE_ID || !env.FLY_API_TOKEN) {
    throw new Error("Fly-Verbindung ist noch nicht vollständig konfiguriert.");
  }
  const app = encodeURIComponent(env.FLY_APP);
  const machine = encodeURIComponent(env.FLY_MACHINE_ID);
  const machineUrl = `https://api.machines.dev/v1/apps/${app}/machines/${machine}`;
  const headers = {
    Authorization: flyAuthorization(env.FLY_API_TOKEN),
    "Content-Type": "application/json",
  };
  const statusResponse = await fetch(machineUrl, { headers });
  if (!statusResponse.ok) {
    const error = new Error(`Fly-Status: HTTP ${statusResponse.status}`);
    error.code = [401, 403].includes(statusResponse.status) ? "fly_auth_rejected" : "fly_api_failed";
    throw error;
  }
  const machineStatus = await statusResponse.json();
  if (["started", "starting", "stopping"].includes(machineStatus.state)) {
    await updateStatus(env, { machine_state: machineStatus.state }).catch(() => {});
    return { started: false, reason: "Machine läuft bereits", state: machineStatus.state };
  }

  const requestedAt = new Date().toISOString();
  // Mark the run as starting *before* the start request. A very fast Fly job can
  // otherwise report completion while this function is still waiting.
  try {
    await updateStatus(env, {
      machine_state: "starting",
      last_start_request: { requested_at: requestedAt, trigger },
      last_start_error: null,
    });
  } catch {
    const error = new Error("Der Startstatus wird noch gespeichert. Bitte erneut versuchen.");
    error.code = "status_storage_busy";
    throw error;
  }

  const startResponse = await fetch(`${machineUrl}/start`, { method: "POST", headers });
  if (!startResponse.ok && startResponse.status !== 409) {
    const body = (await startResponse.text()).slice(0, 512);
    if (isFlyMachineTransition(startResponse.status, body)) {
      const error = new Error("Fly.io aktualisiert die Sync-Machine noch.");
      error.code = "fly_machine_transition";
      throw error;
    }
    const error = new Error(`Fly-Start: HTTP ${startResponse.status}`);
    error.code = "fly_api_failed";
    throw error;
  }
  return { started: true, state: "starting", requested_at: requestedAt };
}

function localizedErrorMessage(request, message) {
  const raw = String(message || "");
  if (!(request.headers.get("X-UI-Lang") || "").toLowerCase().startsWith("en")) return raw;
  const exact = new Map([
    ["Konfiguration fehlt.", "Configuration is missing."],
    ["Ungültiges Intervall.", "Invalid interval."],
    ["Rückblick muss zwischen 1 und 3650 Tagen liegen.", "Lookback must be between 1 and 3650 days."],
    ["Dateigröße muss zwischen 1 und 95 MB liegen.", "File size must be between 1 and 95 MB."],
    ["Mindestens ein Dateityp ist erforderlich.", "At least one file type is required."],
    ["Es sind 1 bis 20 Ordnerzuordnungen erforderlich.", "Between 1 and 20 folder mappings are required."],
    ["Jede Zuordnung braucht Name, IServ-Pfad und reMarkable-Ziel ab /.", "Every mapping needs a name, IServ path, and reMarkable destination starting with /."],
    ["'..' ist in Ordnerpfaden nicht erlaubt.", "'..' is not allowed in folder paths."],
    ["Fly-Verbindung ist noch nicht vollständig konfiguriert.", "Fly connection is not fully configured yet."],
    ["Ungültige Herkunft.", "Invalid origin."],
    ["Ungültige Eingabe.", "Invalid input."],
    ["Start fehlgeschlagen.", "Start failed."],
    ["Nicht gefunden.", "Not found."],
  ]);
  if (exact.has(raw)) return exact.get(raw);
  let match = raw.match(/^Der Name '(.+)' ist doppelt\.$/);
  if (match) return `The name '${match[1]}' is duplicated.`;
  match = raw.match(/^Höchstens acht Zeitfenster pro Wochentag sind erlaubt \((.+)\)\.$/);
  if (match) return `At most eight time windows are allowed per weekday (${translateDayName(match[1], "en")}).`;
  match = raw.match(/^Ungültiges Zeitfenster bei (.+): (.+)$/);
  if (match) return `Invalid time window for ${translateDayName(match[1], "en")}: ${match[2]}`;
  if (raw === "Machine läuft bereits") return "Machine is already running";
  return raw;
}

function translateDayName(value, lang) {
  if (lang !== "en") return value;
  return ({ Montag:"Monday", Dienstag:"Tuesday", Mittwoch:"Wednesday", Donnerstag:"Thursday", Freitag:"Friday", Samstag:"Saturday", Sonntag:"Sunday", "bisherigem Wochenplan":"previous weekly schedule" })[value] || value;
}

async function handleAdminApi(request, env, pathname) {
  const authStatus = await adminAuthorizationStatus(request, env);
  if (authStatus === 429) {
    return json({ error: localized(request, "Zu viele fehlgeschlagene Anmeldeversuche. Bitte später erneut versuchen.", "Too many failed authentication attempts. Try again later.") }, 429);
  }
  if (authStatus !== 200) return unauthorized();
  if (pathname === "/api/admin/settings" && request.method === "GET") {
    const [settings, status] = await Promise.all([getSettings(env), getStatus(env)]);
    return json({ settings, status });
  }
  if (pathname === "/api/admin/recovery" && request.method === "GET") {
    const origin = new URL(request.url).origin;
    return json({
      format: 1,
      app_name: String(env.FLY_APP || ""),
      worker_name: String(env.INSTALL_WORKER_NAME || ""),
      dashboard_url: origin,
      region: String(env.INSTALL_REGION || ""),
      machine_id: String(env.FLY_MACHINE_ID || ""),
      kv_namespace_id: String(env.INSTALL_KV_ID || ""),
      fly_token_until: String(env.INSTALL_FLY_TOKEN_UNTIL || ""),
      version: String(env.INSTALL_VERSION || ""),
    });
  }
  if (!isSameOrigin(request)) return json({ error: localized(request, "Ungültige Herkunft.", "Invalid origin.") }, 403);
  if (pathname === "/api/admin/settings" && request.method === "PUT") {
    try {
      const settings = validateSettings(await readSmallJSON(request, 256 * 1024));
      await env.CONFIG.put(SETTINGS_KEY, JSON.stringify(settings));
      return json({ ok: true, settings });
    } catch (error) {
      return json({ error: localizedErrorMessage(request, error.message || "Ungültige Eingabe.") }, 400);
    }
  }
  if (pathname === "/api/admin/run" && request.method === "POST") {
    try {
      const result = await startMachine(env, "manual");
      if (result.reason) result.reason = localizedErrorMessage(request, result.reason);
      return json({ ok: true, result });
    } catch (error) {
      // A start attempt may have written the same KV key moments ago.
      await updateStatus(env, {
        machine_state: "start_failed",
        last_start_error: String(error.message || error).slice(0, 500),
      }).catch(() => {});
      const code = error?.code === "fly_auth_rejected"
        ? "fly_auth_rejected"
        : (["fly_machine_transition", "status_storage_busy"].includes(error?.code) ? error.code : "fly_start_failed");
      const message = code === "fly_auth_rejected"
        ? localized(request,
          "Die Fly-Steuerberechtigung wurde abgelehnt. Öffne den Setup-Assistenten und setze die Installation beziehungsweise das Cloud-Update fort.",
          "The Fly control credential was rejected. Open the setup assistant and continue installation or the cloud update.")
        : (code === "fly_machine_transition"
          ? localized(request,
            "Fly.io aktualisiert die Sync-Machine noch. Der Setup-Assistent wartet automatisch und versucht den sicheren Start erneut.",
            "Fly.io is still updating the sync Machine. The setup assistant waits and retries the safe start automatically.")
          : localizedErrorMessage(request, error.message || "Start fehlgeschlagen."));
      return json({ error: message, code }, ["fly_machine_transition", "status_storage_busy"].includes(code) ? 503 : 502);
    }
  }
  return json({ error: localized(request, "Nicht gefunden.", "Not found.") }, 404);
}

function normalizeReportStats(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const keys = ["checked", "uploaded", "unchanged", "deferred", "failed", "would_upload"];
  return Object.fromEntries(keys.map((key) => {
    const number = Number(value[key]);
    return [key, Number.isFinite(number) && number >= 0 ? Math.floor(number) : 0];
  }));
}

async function handleAgentApi(request, env, pathname) {
  if (!isAgent(request, env)) return json({ error: "Nicht autorisiert." }, 401);
  if (pathname === "/api/sync-config" && request.method === "GET") {
    const settings = await getSettings(env, null);
    if (!settings) return json({ error: "Konfiguration ist noch nicht verfügbar." }, 503);
    return json(settings.sync);
  }
  if (pathname === "/api/report" && request.method === "POST") {
    let safeReport;
    try {
      const report = await readSmallJSON(request, 64 * 1024);
      const requestedOutcome = cleanText(report.outcome, 20);
      const outcome = ["success", "partial", "failed"].includes(requestedOutcome)
        ? requestedOutcome
        : (Boolean(report.ok) ? "success" : "failed");
      safeReport = {
        finished_at: cleanText(report.finished_at, 40) || new Date().toISOString(),
        ok: Boolean(report.ok),
        dry_run: Boolean(report.dry_run),
        duration_seconds: Number(report.duration_seconds) || 0,
        stats: normalizeReportStats(report.stats),
        outcome,
        reason: cleanText(report.reason, 40) || null,
        message: cleanText(report.message, 500) || null,
        cooldown_until: cleanText(report.cooldown_until, 40) || null,
        error: cleanText(report.error, 500) || null,
      };
    } catch {
      return json({ error: "Ungültiger Bericht." }, 400);
    }
    try {
      await updateStatus(env, {
        machine_state: safeReport.outcome === "failed" ? "failed" : (safeReport.outcome === "partial" ? "completed_partial" : "completed"),
        last_report: safeReport,
        last_start_error: null,
      });
    } catch {
      return json({ error: "Bericht konnte vorübergehend nicht gespeichert werden." }, 503);
    }
    return json({ ok: true });
  }
  return json({ error: "Nicht gefunden." }, 404);
}

async function handleFetch(request, env) {
  const url = new URL(request.url);
  if (url.pathname === "/healthz") return json({ ok: true });
  if (url.pathname === "/ui.js") {
    if (!(await validSession(request, env))) return new Response("", { status: 401, headers: JSON_HEADERS });
    return new Response(DASHBOARD_JS, { headers: { "Content-Type": "text/javascript; charset=utf-8", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff" } });
  }
  if (url.pathname === "/api/login") return handleLogin(request, env);
  if (url.pathname === "/api/logout") return handleLogout(request);
  if (url.pathname.startsWith("/api/admin/")) return handleAdminApi(request, env, url.pathname);
  if (url.pathname === "/api/sync-config" || url.pathname === "/api/report") return handleAgentApi(request, env, url.pathname);
  if (url.pathname !== "/") return new Response("Not found", { status: 404 });
  const commonHeaders = {
    "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store",
    "Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY",
    "Strict-Transport-Security": "max-age=31536000",
    "Permissions-Policy": "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
    "Cross-Origin-Opener-Policy": "same-origin", "Cross-Origin-Resource-Policy": "same-origin",
  };
  if (!(await validSession(request, env))) {
    const nonceBytes = new Uint8Array(16);
    crypto.getRandomValues(nonceBytes);
    const nonce = base64Url(nonceBytes);
    const loginHTML = LOGIN_HTML.replace("<script>", `<script nonce="${nonce}">`);
    return new Response(loginHTML, { headers: {
      ...commonHeaders,
      "Content-Security-Policy": `default-src 'self'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`,
    } });
  }
  return new Response(DASHBOARD_HTML, { headers: {
    ...commonHeaders,
    "Content-Security-Policy": "default-src 'self'; style-src 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'",
  } });
}

export default {
  fetch: handleFetch,
  async scheduled(controller, env, ctx) {
    ctx.waitUntil(
      (async () => {
        const scheduledDate = new Date(controller.scheduledTime);
        const settings = await getSettings(env);
        if (!isDue(settings, scheduledDate)) return;
        const slot = berlinSlot(scheduledDate);
        const status = await getStatus(env);
        if (status.last_schedule_slot === slot) return;
        await updateStatus(env, { last_schedule_slot: slot });
        try {
          await startMachine(env, "schedule");
        } catch (error) {
          await updateStatus(env, {
            machine_state: "start_failed",
            last_start_error: String(error.message || error).slice(0, 500),
          });
          throw error;
        }
      })(),
    );
  },
};

export { DEFAULT_SETTINGS, flyAuthorization, isDue, isFlyMachineTransition, validateSettings };
