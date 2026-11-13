import test from "node:test";
import assert from "node:assert/strict";
import worker, { DEFAULT_SETTINGS, flyAuthorization, isDue, isFlyMachineTransition, validateSettings } from "./index.js";

function enabledSettings() {
  return { ...structuredClone(DEFAULT_SETTINGS), enabled: true };
}

function fakeEnv(seed = {}) {
  const values = new Map();
  for (const [key, value] of Object.entries(seed)) {
    values.set(key, typeof value === "string" ? value : JSON.stringify(value));
  }
  return {
    ADMIN_PASSWORD: "sehr-langes-testpasswort",
    SYNC_API_TOKEN: "agent-test-token",
    CONFIG: {
      async get(key, type) {
        const value = values.get(key);
        return type === "json" && value ? JSON.parse(value) : value ?? null;
      },
      async put(key, value) { values.set(key, value); },
      async delete(key) { values.delete(key); },
    },
  };
}

function adminAuthorization(env) {
  return `Basic ${btoa(`admin:${env.ADMIN_PASSWORD}`)}`;
}

test("Fly Machines API authorization preserves the complete FlyV1 header", () => {
  assert.equal(flyAuthorization("FlyV1 fm2_test_token"), "FlyV1 fm2_test_token");
  assert.equal(
    flyAuthorization("FlyV1 fm2_permission_token,fm2_discharge_token"),
    "FlyV1 fm2_permission_token,fm2_discharge_token",
  );
  assert.throws(() => flyAuthorization("Bearer FlyV1 fm2_test_token"), (error) => {
    assert.equal(error.code, "fly_auth_rejected");
    return /ungültiges Format/.test(error.message);
  });
  assert.throws(() => flyAuthorization("FlyV1 short"), /ungültiges Format/);
  assert.throws(() => flyAuthorization("FlyV1 fm2_test_token,"), /ungültiges Format/);
  assert.throws(() => flyAuthorization("FlyV1 fm2_test_token,short"), /ungültiges Format/);
  assert.throws(() => flyAuthorization("FlyV1 fm2_test_token\n"), /ungültiges Format/);
  assert.throws(() => flyAuthorization("FlyV1 fm2_test_token\nInjected: value"), /ungültiges Format/);
});

test("only Fly's exact Machine replacement precondition is retryable", () => {
  assert.equal(isFlyMachineTransition(412, JSON.stringify({
    error: "failed_precondition: machine getting replaced, refusing to start",
  })), true);
  assert.equal(isFlyMachineTransition(412, JSON.stringify({ error: "another precondition" })), false);
  assert.equal(isFlyMachineTransition(503, JSON.stringify({
    error: "failed_precondition: machine getting replaced, refusing to start",
  })), false);
  assert.equal(isFlyMachineTransition(412, "not-json"), false);
});

test("Machine replacement start race is private and explicitly retryable", async () => {
  const env = fakeEnv();
  const put = env.CONFIG.put;
  let statusWrites = 0;
  env.CONFIG.put = async (key, value) => {
    if (key === "status-v2" && ++statusWrites === 2) throw new Error("429: KV write limit");
    await put(key, value);
  };
  env.FLY_APP = "iserv-rm-deadbeef";
  env.FLY_MACHINE_ID = "18590d2a341e18";
  env.FLY_API_TOKEN = "FlyV1 fm2_test_token";
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => {
    calls += 1;
    if (calls === 1) return new Response(JSON.stringify({ state: "stopped" }), { status: 200 });
    return new Response(JSON.stringify({
      error: "failed_precondition: machine getting replaced, refusing to start",
    }), { status: 412 });
  };
  try {
    const response = await worker.fetch(new Request("https://example.test/api/admin/run", {
      method: "POST",
      headers: {
        Authorization: adminAuthorization(env),
        Origin: "https://example.test",
        "Content-Type": "application/json",
      },
      body: "{}",
    }), env);
    assert.equal(response.status, 503);
    assert.equal(statusWrites, 2);
    const body = await response.json();
    assert.equal(body.code, "fly_machine_transition");
    assert.match(body.error, /automatisch/);
    assert.equal(JSON.stringify(body).includes("failed_precondition"), false);
    assert.equal(JSON.stringify(body).includes(env.FLY_API_TOKEN), false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("invalid stored Fly credentials use the resumable repair path", async () => {
  const env = fakeEnv();
  env.FLY_APP = "iserv-rm-deadbeef";
  env.FLY_MACHINE_ID = "18590d2a341e18";
  env.FLY_API_TOKEN = "Bearer FlyV1 fm2_test_token";
  const response = await worker.fetch(new Request("https://example.test/api/admin/run", {
    method: "POST",
    headers: {
      Authorization: adminAuthorization(env),
      Origin: "https://example.test",
      "Content-Type": "application/json",
    },
    body: "{}",
  }), env);
  assert.equal(response.status, 502);
  const body = await response.json();
  assert.equal(body.code, "fly_auth_rejected");
  assert.match(body.error, /Setup-Assistenten/);
  assert.equal(JSON.stringify(body).includes(env.FLY_API_TOKEN), false);
});

test("a pending start-status write is retryable before Fly is started", async () => {
  const env = fakeEnv();
  env.FLY_APP = "iserv-rm-deadbeef";
  env.FLY_MACHINE_ID = "18590d2a341e18";
  env.FLY_API_TOKEN = "FlyV1 fm2_test_token";
  env.CONFIG.put = async () => { throw new Error("KV write limit"); };
  const originalFetch = globalThis.fetch;
  let reads = 0;
  globalThis.fetch = async (_url, init) => {
    assert.notEqual(init.method, "POST");
    reads++;
    return new Response(JSON.stringify({ state: "stopped" }));
  };
  try {
    const response = await worker.fetch(new Request("https://example.test/api/admin/run", {
      method: "POST",
      headers: { Authorization: adminAuthorization(env), Origin: "https://example.test" },
      body: "{}",
    }), env);
    assert.equal(response.status, 503);
    assert.equal((await response.json()).code, "status_storage_busy");
    assert.equal(reads, 1);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("Fly authorization rejection is actionable and does not expose provider details", async () => {
  const env = fakeEnv();
  env.FLY_APP = "iserv-rm-deadbeef";
  env.FLY_MACHINE_ID = "18590d2a341e18";
  env.FLY_API_TOKEN = "FlyV1 fm2_permission_token,fm2_discharge_token";
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (_input, init) => {
    assert.equal(new Headers(init.headers).get("Authorization"), env.FLY_API_TOKEN);
    return new Response("provider-private-detail", { status: 401 });
  };
  try {
    const response = await worker.fetch(new Request("https://example.test/api/admin/run", {
      method: "POST",
      headers: {
        Authorization: adminAuthorization(env),
        Origin: "https://example.test",
        "Content-Type": "application/json",
      },
      body: "{}",
    }), env);
    assert.equal(response.status, 502);
    const body = await response.json();
    assert.equal(body.code, "fly_auth_rejected");
    assert.match(body.error, /Setup-Assistenten/);
    assert.equal(JSON.stringify(body).includes("provider-private-detail"), false);
    assert.equal(JSON.stringify(body).includes(env.FLY_API_TOKEN), false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("non-authentication Fly failures remain separately classified", async () => {
  const env = fakeEnv();
  env.FLY_APP = "iserv-rm-deadbeef";
  env.FLY_MACHINE_ID = "18590d2a341e18";
  env.FLY_API_TOKEN = "FlyV1 fm2_test_token";
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => new Response("provider-private-detail", { status: 503 });
  try {
    const response = await worker.fetch(new Request("https://example.test/api/admin/run", {
      method: "POST",
      headers: {
        Authorization: adminAuthorization(env),
        Origin: "https://example.test",
        "Content-Type": "application/json",
      },
      body: "{}",
    }), env);
    assert.equal(response.status, 502);
    const body = await response.json();
    assert.equal(body.code, "fly_start_failed");
    assert.equal(JSON.stringify(body).includes("provider-private-detail"), false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});


test("public health endpoint exposes no secrets", async () => {
  const env = fakeEnv();
  const response = await worker.fetch(new Request("https://example.test/healthz"), env);
  assert.equal(response.status, 200);
  const body = await response.json();
  assert.deepEqual(body, { ok: true });
  const raw = JSON.stringify(body);
  assert.equal(raw.includes(env.ADMIN_PASSWORD), false);
  assert.equal(raw.includes(env.SYNC_API_TOKEN), false);
});
test("default configuration is safe, disabled, and has separate weekday schedules", () => {
  assert.equal(DEFAULT_SETTINGS.enabled, false);
  assert.equal(DEFAULT_SETTINGS.sync.dry_run, true);
  assert.equal(DEFAULT_SETTINGS.schedule["1"].length, 2);
  assert.equal(DEFAULT_SETTINGS.schedule["5"].length, 2);
  assert.equal(DEFAULT_SETTINGS.schedule["6"].length, 0);
  assert.equal(DEFAULT_SETTINGS.schedule["7"].length, 0);
});

test("winter and summer times use Europe/Berlin", () => {
  const settings = enabledSettings();
  assert.equal(isDue(settings, new Date("2026-01-12T06:30:00Z")), true);
  assert.equal(isDue(settings, new Date("2026-08-17T05:30:00Z")), true);
});

test("each weekday can have different time windows", () => {
  const settings = enabledSettings();
  settings.schedule = {
    "1": [{ start: "07:30", end: "08:30" }],
    "2": [{ start: "10:00", end: "11:00" }],
    "3": [], "4": [], "5": [], "6": [], "7": [],
  };
  assert.equal(isDue(settings, new Date("2026-08-17T05:30:00Z")), true); // Monday 07:30
  assert.equal(isDue(settings, new Date("2026-08-17T08:00:00Z")), false); // Monday 10:00
  assert.equal(isDue(settings, new Date("2026-08-18T08:00:00Z")), true); // Tuesday 10:00
});

test("a weekday without time windows is inactive", () => {
  const settings = enabledSettings();
  settings.schedule["1"] = [];
  assert.equal(isDue(settings, new Date("2026-08-17T05:30:00Z")), false);
});

test("interval is counted from beginning of the selected day's window", () => {
  const settings = enabledSettings();
  assert.equal(isDue(settings, new Date("2026-01-12T06:40:00Z")), true);
  assert.equal(isDue(settings, new Date("2026-01-12T06:41:00Z")), false);
});

test("validation rejects malformed daily windows", () => {
  const settings = enabledSettings();
  settings.schedule["3"] = [{ start: "14:00", end: "13:00" }];
  assert.throws(() => validateSettings(settings), /Mittwoch/);
});

test("validation rejects unsafe or malformed reMarkable paths", () => {
  const settings = enabledSettings();
  settings.sync.sources[0].remarkable_dir = "IServ/Mathe";
  assert.throws(() => validateSettings(settings));
});

test("unauthenticated browser receives styled login page", async () => {
  const response = await worker.fetch(new Request("https://example.test/"), fakeEnv());
  assert.equal(response.status, 200);
  const html = await response.text();
  assert.match(html, /Dashboard-Passwort/);
  assert.match(html, /Sign in/);
});

test("browser login creates session cookie accepted by admin API", async () => {
  const env = fakeEnv();
  const login = await worker.fetch(new Request("https://example.test/api/login", {
    method: "POST",
    headers: { Origin: "https://example.test", "Content-Type": "application/json", "X-UI-Lang": "en" },
    body: JSON.stringify({ password: env.ADMIN_PASSWORD }),
  }), env);
  assert.equal(login.status, 200);
  const cookie = login.headers.get("Set-Cookie");
  assert.match(cookie, /iserv_rm_session=/);
  assert.match(cookie, /HttpOnly/);
  assert.match(cookie, /SameSite=Strict/);
  const pair = cookie.split(";")[0];
  const response = await worker.fetch(new Request("https://example.test/api/admin/settings", { headers: { Cookie: pair } }), env);
  assert.equal(response.status, 200);
});

test("browser login rate limits repeated wrong passwords", async () => {
  const env = fakeEnv();
  for (let i = 0; i < 5; i += 1) {
    const response = await worker.fetch(new Request("https://example.test/api/login", {
      method: "POST", headers: { Origin: "https://example.test", "Content-Type": "application/json", "CF-Connecting-IP": "192.0.2.1" }, body: JSON.stringify({ password: "wrong" }),
    }), env);
    assert.equal(response.status, 401);
  }
  const blocked = await worker.fetch(new Request("https://example.test/api/login", {
    method: "POST", headers: { Origin: "https://example.test", "Content-Type": "application/json", "CF-Connecting-IP": "192.0.2.1" }, body: JSON.stringify({ password: "wrong" }),
  }), env);
  assert.equal(blocked.status, 429);
});

test("dashboard browser surface requires session and uses external-script-only CSP", async () => {
  const env = fakeEnv();
  const basicOnly = await worker.fetch(
    new Request("https://example.test/", { headers: { Authorization: adminAuthorization(env) } }),
    env,
  );
  const basicHTML = await basicOnly.text();
  assert.match(basicHTML, /Dashboard-Passwort/);
  assert.doesNotMatch(basicHTML, /Zeiten auf andere Wochentage anwenden/);

  const login = await worker.fetch(new Request("https://example.test/api/login", {
    method: "POST",
    headers: { Origin: "https://example.test", "Content-Type": "application/json" },
    body: JSON.stringify({ password: env.ADMIN_PASSWORD }),
  }), env);
  const pair = login.headers.get("Set-Cookie").split(";")[0];
  const response = await worker.fetch(new Request("https://example.test/", { headers: { Cookie: pair } }), env);
  assert.equal(response.status, 200);
  assert.equal(response.headers.get("Content-Security-Policy").includes("script-src 'self'"), true);
  assert.equal(response.headers.get("Content-Security-Policy").includes("script-src 'unsafe-inline'"), false);
  const html = await response.text();
  assert.match(html, /IServ → reMarkable/);
  assert.match(html, /Zeiten auf andere Wochentage anwenden/);
  assert.match(html, /id="dayTabs"/);
  assert.match(html, /status-result/);
});

test("legacy weekdays plus shared windows migrate without losing the old schedule", async () => {
  const legacy = structuredClone(DEFAULT_SETTINGS);
  delete legacy.schedule;
  legacy.weekdays = [1, 3, 5];
  legacy.windows = [
    { start: "08:10", end: "12:20" },
    { start: "13:00", end: "14:30" },
  ];
  const env = fakeEnv({ "settings-v2": legacy });
  const response = await worker.fetch(
    new Request("https://example.test/api/admin/settings", {
      headers: { Authorization: adminAuthorization(env) },
    }),
    env,
  );
  assert.equal(response.status, 200);
  const body = await response.json();
  assert.deepEqual(body.settings.schedule["1"], legacy.windows);
  assert.deepEqual(body.settings.schedule["3"], legacy.windows);
  assert.deepEqual(body.settings.schedule["5"], legacy.windows);
  assert.deepEqual(body.settings.schedule["2"], []);
  assert.equal("weekdays" in body.settings, false);
  assert.equal("windows" in body.settings, false);
});

test("settings write needs same origin and preserves safe defaults", async () => {
  const env = fakeEnv();
  const authorization = adminAuthorization(env);
  const denied = await worker.fetch(
    new Request("https://example.test/api/admin/settings", {
      method: "PUT",
      headers: { Authorization: authorization, "Content-Type": "application/json" },
      body: JSON.stringify(DEFAULT_SETTINGS),
    }),
    env,
  );
  assert.equal(denied.status, 403);

  const accepted = await worker.fetch(
    new Request("https://example.test/api/admin/settings", {
      method: "PUT",
      headers: {
        Authorization: authorization,
        Origin: "https://example.test",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(DEFAULT_SETTINGS),
    }),
    env,
  );
  assert.equal(accepted.status, 200);
  const body = await accepted.json();
  assert.equal(body.settings.enabled, false);
  assert.equal(body.settings.sync.dry_run, true);
  assert.equal(body.settings.schedule["1"].length, 2);
});

test("Fly job requires both agent authorization and explicitly saved configuration", async () => {
  const env = fakeEnv();
  const denied = await worker.fetch(new Request("https://example.test/api/sync-config"), env);
  assert.equal(denied.status, 401);
  const request = new Request("https://example.test/api/sync-config", {
    headers: { Authorization: `Bearer ${env.SYNC_API_TOKEN}` },
  });
  const notReady = await worker.fetch(request, env);
  assert.equal(notReady.status, 503);
  assert.equal("sources" in await notReady.json(), false);
  await env.CONFIG.put("settings-v2", JSON.stringify(DEFAULT_SETTINGS));
  const accepted = await worker.fetch(request, env);
  assert.equal(accepted.status, 200);
  const config = await accepted.json();
  assert.equal(config.dry_run, true);
  assert.equal("schedule" in config, false);
});

test("a transient report storage failure is retryable, while malformed reports are rejected", async () => {
  const env = fakeEnv();
  env.CONFIG.put = async () => { throw new Error("429: KV write limit"); };
  const send = (body) => worker.fetch(new Request("https://example.test/api/report", {
    method: "POST",
    headers: { Authorization: `Bearer ${env.SYNC_API_TOKEN}` },
    body,
  }), env);
  assert.equal((await send("{")).status, 400);
  assert.equal((await send(JSON.stringify({ ok: true, dry_run: true }))).status, 503);
});

test("reMarkable target keeps the new IServ subfolder and trims trailing slash", () => {
  const settings = enabledSettings();
  settings.sync.sources[0] = {
    name: "Deutsch",
    iserv_path: "Groups/deutsch-q1",
    remarkable_dir: "/Schule/01 | Deutsch/IServ/",
  };
  const validated = validateSettings(settings);
  assert.equal(validated.sync.sources[0].remarkable_dir, "/Schule/01 | Deutsch/IServ");
});

test("partial Fly report is stored as completed_partial", async () => {
  const env = fakeEnv();
  const reportResponse = await worker.fetch(
    new Request("https://example.test/api/report", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${env.SYNC_API_TOKEN}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        finished_at: "2026-08-19T00:00:00+02:00",
        ok: true,
        dry_run: false,
        outcome: "partial",
        reason: "upload_limit",
        message: "5 Dateien hochgeladen; Rest später.",
        stats: { checked: 38, uploaded: 5, unchanged: 0, deferred: 33, failed: 0 },
      }),
    }),
    env,
  );
  assert.equal(reportResponse.status, 200);

  const adminResponse = await worker.fetch(
    new Request("https://example.test/api/admin/settings", {
      headers: { Authorization: adminAuthorization(env) },
    }),
    env,
  );
  assert.equal(adminResponse.status, 200);
  const body = await adminResponse.json();
  assert.equal(body.status.machine_state, "completed_partial");
  assert.equal(body.status.last_report.outcome, "partial");
  assert.equal(body.status.last_report.stats.deferred, 33);
});

test("authenticated recovery metadata contains only non-secret installation identifiers", async () => {
  const env = fakeEnv();
  env.FLY_APP = "iserv-rm-abcd1234";
  env.FLY_MACHINE_ID = "18590d2a341e18";
  env.INSTALL_WORKER_NAME = "iserv-rm-control-abcd1234";
  env.INSTALL_REGION = "fra";
  env.INSTALL_VERSION = "0.9.0-rc1";
  env.INSTALL_KV_ID = "0123456789abcdef0123456789abcdef";
  env.INSTALL_FLY_TOKEN_UNTIL = "2031-08-18T00:00:00Z";
  env.FLY_API_TOKEN = "fly-token-not-returned";
  const response = await worker.fetch(new Request("https://example.test/api/admin/recovery", {
    headers: { Authorization: adminAuthorization(env) },
  }), env);
  assert.equal(response.status, 200);
  const body = await response.json();
  assert.deepEqual(body, {
    format: 1,
    app_name: "iserv-rm-abcd1234",
    worker_name: "iserv-rm-control-abcd1234",
    dashboard_url: "https://example.test",
    region: "fra",
    machine_id: "18590d2a341e18",
    kv_namespace_id: "0123456789abcdef0123456789abcdef",
    fly_token_until: "2031-08-18T00:00:00Z",
    version: "0.9.0-rc1",
  });
  const raw = JSON.stringify(body);
  assert.equal(raw.includes(env.ADMIN_PASSWORD), false);
  assert.equal(raw.includes(env.SYNC_API_TOKEN), false);
  assert.equal(raw.includes(env.FLY_API_TOKEN), false);
});


test("login page uses nonce CSP instead of unsafe-inline scripts", async () => {
  const response = await worker.fetch(new Request("https://example.test/"), fakeEnv());
  const csp = response.headers.get("Content-Security-Policy") || "";
  assert.match(csp, /script-src 'nonce-[A-Za-z0-9_-]+'/);
  assert.equal(csp.includes("script-src 'unsafe-inline'"), false);
  const html = await response.text();
  assert.match(html, /<script nonce="[A-Za-z0-9_-]+">/);
});

test("admin API fails closed when dashboard password is absent", async () => {
  const env = fakeEnv();
  env.ADMIN_PASSWORD = "";
  const response = await worker.fetch(new Request("https://example.test/api/admin/settings"), env);
  assert.equal(response.status, 401);
});

test("failed Basic admin authentication is throttled without blocking a correct credential", async () => {
  const env = fakeEnv();
  const headers = { "CF-Connecting-IP": "192.0.2.44", Authorization: "Basic Zm9vOmJhcg==" };
  for (let i = 0; i < 5; i += 1) {
    const response = await worker.fetch(new Request("https://example.test/api/admin/settings", { headers }), env);
    assert.equal(response.status, 401);
  }
  const blocked = await worker.fetch(new Request("https://example.test/api/admin/settings", { headers }), env);
  assert.equal(blocked.status, 429);

  const accepted = await worker.fetch(new Request("https://example.test/api/admin/settings", {
    headers: { "CF-Connecting-IP": "192.0.2.44", Authorization: adminAuthorization(env) },
  }), env);
  assert.equal(accepted.status, 200);
});

test("multibyte JSON body limit is enforced in bytes", async () => {
  const env = fakeEnv();
  const password = "ä".repeat(2500);
  const response = await worker.fetch(new Request("https://example.test/api/login", {
    method: "POST",
    headers: { Origin: "https://example.test", "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
  }), env);
  assert.equal(response.status, 413);
});

test("agent report stores only normalized numeric counters", async () => {
  const env = fakeEnv();
  const response = await worker.fetch(new Request("https://example.test/api/report", {
    method: "POST",
    headers: { Authorization: `Bearer ${env.SYNC_API_TOKEN}`, "Content-Type": "application/json" },
    body: JSON.stringify({ ok: true, outcome: "success", stats: { checked: 3.9, uploaded: "2", failed: -7, injected: { secret: true } } }),
  }), env);
  assert.equal(response.status, 200);
  const state = await worker.fetch(new Request("https://example.test/api/admin/settings", {
    headers: { Authorization: adminAuthorization(env) },
  }), env);
  const body = await state.json();
  assert.deepEqual(body.status.last_report.stats, { checked: 3, uploaded: 2, unchanged: 0, deferred: 0, failed: 0, would_upload: 0 });
  assert.equal("injected" in body.status.last_report.stats, false);
});


test("path validation rejects control characters and traversal but allows ordinary double dots", () => {
  const settings = enabledSettings();
  settings.sync.sources[0].iserv_path = "Groups/Material..alt";
  assert.doesNotThrow(() => validateSettings(settings));
  for (const bad of ["Groups/A/../B", "Groups/A//B", "Groups/A\\B", "Groups/A\nB"]) {
    const candidate = enabledSettings();
    candidate.sync.sources[0].iserv_path = bad;
    assert.throws(() => validateSettings(candidate));
  }
});

test("dashboard HTML sends anti-framing and browser hardening headers", async () => {
  const response = await worker.fetch(new Request("https://example.test/"), fakeEnv());
  assert.equal(response.headers.get("X-Frame-Options"), "DENY");
  assert.match(response.headers.get("Content-Security-Policy") || "", /frame-ancestors 'none'/);
  assert.equal(response.headers.get("Cross-Origin-Opener-Policy"), "same-origin");
  assert.match(response.headers.get("Permissions-Policy") || "", /camera=\(\)/);
});

test("oversized dashboard login body is rejected", async () => {
  const env = fakeEnv();
  const response = await worker.fetch(new Request("https://example.test/api/login", {
    method: "POST",
    headers: { Origin: "https://example.test", "Content-Type": "application/json", "Content-Length": "5000" },
    body: JSON.stringify({ password: "x".repeat(4900) }),
  }), env);
  assert.equal(response.status, 413);
});
