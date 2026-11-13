import worker from "./index.js";
import { DASHBOARD_ENHANCEMENT_JS } from "./dashboard_enhancement.js";

const ACTIVATION_KEY = "activation-v1";
const SETTINGS_KEY = "settings-v2";
const STATUS_KEY = "status-v2";

const JSON_HEADERS = {
  "Content-Type": "application/json; charset=utf-8",
  "Cache-Control": "no-store",
  "X-Content-Type-Options": "nosniff",
};

function json(value, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: JSON_HEADERS });
}

function localized(request, de, en) {
  return (request.headers.get("X-UI-Lang") || "").toLowerCase().startsWith("en") ? en : de;
}

function isSameOrigin(request) {
  const origin = request.headers.get("Origin");
  return origin === new URL(request.url).origin;
}

function usesBasicAdmin(request) {
  return (request.headers.get("Authorization") || "").startsWith("Basic ");
}

async function readSmallJSON(request, maxBytes) {
  const length = Number(request.headers.get("Content-Length") || 0);
  if (Number.isFinite(length) && length > maxBytes) throw new Error("request_too_large");
  const raw = await request.arrayBuffer();
  if (raw.byteLength > maxBytes) throw new Error("request_too_large");
  const text = new TextDecoder("utf-8", { fatal: true }).decode(raw);
  return text ? JSON.parse(text) : {};
}

function validActivation(value) {
  return Boolean(
    value &&
    typeof value === "object" &&
    value.version === 1 &&
    typeof value.real_run_verified === "boolean" &&
    typeof value.enabled === "boolean",
  );
}

function reportShowsLegacyRealUse(report) {
  return Boolean(
    report &&
    report.dry_run === false &&
    ["success", "partial"].includes(report.outcome) &&
    Number(report?.stats?.failed || 0) === 0,
  );
}

async function activationState(env) {
  let stored = null;
  try {
    stored = await env.CONFIG.get(ACTIVATION_KEY, "json");
  } catch {
    stored = { invalid: true };
  }
  if (validActivation(stored)) return stored;

  // Once an activation record exists, malformed state fails closed instead of
  // being re-inferred from temporarily real-mode settings.
  if (stored !== null) {
    const locked = {
      version: 1,
      real_run_verified: false,
      enabled: false,
      state_recovered_fail_closed: true,
      updated_at: new Date().toISOString(),
    };
    await env.CONFIG.put(ACTIVATION_KEY, JSON.stringify(locked));
    return locked;
  }

  // Existing pre-gate installations must keep working. A previously enabled
  // schedule, non-dry-run configuration, or successful historical real run is
  // treated as already activated. A fresh safe installation starts locked.
  const [settings, status] = await Promise.all([
    env.CONFIG.get(SETTINGS_KEY, "json"),
    env.CONFIG.get(STATUS_KEY, "json"),
  ]);
  const legacyActive = Boolean(
    settings?.enabled ||
    settings?.sync?.dry_run === false ||
    reportShowsLegacyRealUse(status?.last_report),
  );
  const initial = {
    version: 1,
    real_run_verified: legacyActive,
    enabled: legacyActive,
    migrated_legacy: legacyActive,
    updated_at: new Date().toISOString(),
  };
  await env.CONFIG.put(ACTIVATION_KEY, JSON.stringify(initial));
  return initial;
}

async function saveActivation(env, patch) {
  const current = await activationState(env);
  const next = {
    ...current,
    ...patch,
    version: 1,
    updated_at: new Date().toISOString(),
  };
  await env.CONFIG.put(ACTIVATION_KEY, JSON.stringify(next));
  return next;
}

function successfulRealReport(report) {
  const stats = report?.stats;
  return Boolean(
    report &&
    typeof report === "object" &&
    !Array.isArray(report) &&
    stats &&
    typeof stats === "object" &&
    !Array.isArray(stats) &&
    report.ok === true &&
    report.dry_run === false &&
    ["success", "partial"].includes(report.outcome) &&
    Number(stats.failed || 0) === 0 &&
    Number(stats.uploaded || 0) > 0,
  );
}

async function authorizeAdmin(request, env) {
  const target = new URL("/api/admin/settings", request.url);
  const headers = new Headers();
  for (const name of ["Authorization", "Cookie", "CF-Connecting-IP", "X-UI-Lang"]) {
    const value = request.headers.get(name);
    if (value) headers.set(name, value);
  }
  return worker.fetch(new Request(target, { method: "GET", headers }), env);
}

async function responseWithActivation(response, env) {
  if (!response.ok) return response;
  const body = await response.json();
  return json({ ...body, activation: await activationState(env) }, response.status);
}

async function handleSettingsPut(request, env) {
  if (!isSameOrigin(request)) return worker.fetch(request, env);

  const authorization = await authorizeAdmin(request, env);
  if (!authorization.ok) return authorization;

  let candidate;
  try {
    candidate = await readSmallJSON(request.clone(), 256 * 1024);
  } catch (error) {
    if (error?.message === "request_too_large") {
      return json({ error: localized(request, "Anfrage ist zu groß.", "Request is too large.") }, 413);
    }
    return worker.fetch(request, env);
  }

  const activation = await activationState(env);
  const basic = usesBasicAdmin(request);
  const wantsEnabled = Boolean(candidate?.enabled);
  const wantsRealMode = candidate?.sync?.dry_run === false;

  if (!activation.enabled) {
    if (wantsEnabled && !(basic && activation.real_run_verified)) {
      return json({ error: localized(
        request,
        "Automatik bleibt gesperrt, bis ein begrenzter echter Lauf mindestens eine Datei erfolgreich hochgeladen hat.",
        "Automation remains locked until a limited real run has successfully uploaded at least one file.",
      ) }, 409);
    }
    if (wantsRealMode && !basic) {
      return json({ error: localized(
        request,
        "Der Testmodus ist während der sicheren Ersteinrichtung gesperrt. Schließe den begrenzten echten Lauf im Setup-Assistenten ab.",
        "Test mode is locked during safe initial setup. Complete the limited real run in the setup companion.",
      ) }, 409);
    }
  }

  const response = await worker.fetch(request, env);
  if (response.ok && wantsEnabled && basic && activation.real_run_verified && !activation.enabled) {
    await saveActivation(env, { enabled: true, enabled_at: new Date().toISOString() });
  }
  return response;
}

async function handleReport(request, env) {
  let report = null;
  try {
    report = await readSmallJSON(request.clone(), 64 * 1024);
  } catch {
    // The underlying Worker remains authoritative for validation and errors.
  }
  const response = await worker.fetch(request, env);
  if (response.ok && successfulRealReport(report)) {
    const current = await activationState(env);
    if (!current.real_run_verified) {
      await saveActivation(env, {
        real_run_verified: true,
        verified_at: new Date().toISOString(),
      });
    }
  }
  return response;
}

async function enhancedUI(request, env) {
  const response = await worker.fetch(request, env);
  if (!response.ok) return response;
  const source = await response.text();
  const headers = new Headers(response.headers);
  headers.delete("Content-Length");
  return new Response(source + DASHBOARD_ENHANCEMENT_JS, {
    status: response.status,
    headers,
  });
}

async function handleFetch(request, env) {
  const url = new URL(request.url);

  if (url.pathname === "/ui.js" && request.method === "GET") {
    return enhancedUI(request, env);
  }

  if (url.pathname === "/api/admin/settings" && request.method === "GET") {
    return responseWithActivation(await worker.fetch(request, env), env);
  }

  if (url.pathname === "/api/admin/settings" && request.method === "PUT") {
    return handleSettingsPut(request, env);
  }

  if (url.pathname === "/api/admin/run" && request.method === "POST" && !usesBasicAdmin(request)) {
    const authorization = await authorizeAdmin(request, env);
    if (!authorization.ok) return authorization;
    const activation = await activationState(env);
    if (!activation.enabled) {
      const settings = await env.CONFIG.get(SETTINGS_KEY, "json");
      if (settings?.sync?.dry_run === false) {
        return json({ error: localized(
          request,
          "Ein echter manueller Lauf ist vor Abschluss der sicheren Ersteinrichtung gesperrt.",
          "A real manual run is locked until safe initial setup is complete.",
        ) }, 409);
      }
    }
  }

  if (url.pathname === "/api/report" && request.method === "POST") {
    return handleReport(request, env);
  }

  return worker.fetch(request, env);
}

export default {
  fetch: handleFetch,
  async scheduled(controller, env, ctx) {
    const activation = await activationState(env);
    if (!activation.enabled) return;
    return worker.scheduled(controller, env, ctx);
  },
};

export { activationState, successfulRealReport };
