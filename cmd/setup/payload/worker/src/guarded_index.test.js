import test from "node:test";
import assert from "node:assert/strict";
import guardedWorker, { activationState, successfulRealReport } from "./guarded_index.js";
import { DEFAULT_SETTINGS } from "./index.js";

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

function basic(env) {
  return `Basic ${btoa(`admin:${env.ADMIN_PASSWORD}`)}`;
}

async function browserSession(env) {
  const login = await guardedWorker.fetch(new Request("https://example.test/api/login", {
    method: "POST",
    headers: { Origin: "https://example.test", "Content-Type": "application/json" },
    body: JSON.stringify({ password: env.ADMIN_PASSWORD }),
  }), env);
  assert.equal(login.status, 200);
  return login.headers.get("Set-Cookie").split(";")[0];
}

async function putSettings(env, settings, authorization) {
  return guardedWorker.fetch(new Request("https://example.test/api/admin/settings", {
    method: "PUT",
    headers: {
      ...(authorization.startsWith("Basic ") ? { Authorization: authorization } : { Cookie: authorization }),
      Origin: "https://example.test",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(settings),
  }), env);
}

test("fresh installations start with activation locked", async () => {
  const env = fakeEnv();
  const state = await activationState(env);
  assert.equal(state.real_run_verified, false);
  assert.equal(state.enabled, false);
});

test("legacy active installations migrate without being unexpectedly disabled", async () => {
  const legacy = structuredClone(DEFAULT_SETTINGS);
  legacy.enabled = true;
  legacy.sync.dry_run = false;
  const env = fakeEnv({ "settings-v2": legacy });
  const state = await activationState(env);
  assert.equal(state.real_run_verified, true);
  assert.equal(state.enabled, true);
  assert.equal(state.migrated_legacy, true);
});

test("legacy successful real report also preserves an intentionally paused installation", async () => {
  const paused = structuredClone(DEFAULT_SETTINGS);
  const env = fakeEnv({
    "settings-v2": paused,
    "status-v2": {
      machine_state: "completed",
      last_report: { dry_run: false, outcome: "success", stats: { uploaded: 0, failed: 0 } },
    },
  });
  const state = await activationState(env);
  assert.equal(state.enabled, true);
  assert.equal(state.real_run_verified, true);
});

test("malformed activation state fails closed instead of re-inferring an unlocked state", async () => {
  const activeSettings = structuredClone(DEFAULT_SETTINGS);
  activeSettings.enabled = true;
  activeSettings.sync.dry_run = false;
  const env = fakeEnv({
    "settings-v2": activeSettings,
    "activation-v1": { version: 99, enabled: true },
  });
  const state = await activationState(env);
  assert.equal(state.real_run_verified, false);
  assert.equal(state.enabled, false);
  assert.equal(state.state_recovered_fail_closed, true);
});

test("dashboard GET includes activation state", async () => {
  const env = fakeEnv({ "settings-v2": DEFAULT_SETTINGS });
  const response = await guardedWorker.fetch(new Request("https://example.test/api/admin/settings", {
    headers: { Authorization: basic(env) },
  }), env);
  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.activation.enabled, false);
  assert.equal(body.activation.real_run_verified, false);
});

test("dashboard JavaScript includes the permanent interaction polish layer", async () => {
  const env = fakeEnv({ "settings-v2": DEFAULT_SETTINGS });
  const cookie = await browserSession(env);
  const response = await guardedWorker.fetch(new Request("https://example.test/ui.js", {
    headers: { Cookie: cookie },
  }), env);
  assert.equal(response.status, 200);
  const source = await response.text();
  assert.match(source, /Product-polish layer/);
  assert.match(source, /Unsaved changes/);
  assert.match(source, /activation/);
});

test("browser cannot disable dry-run or enable automation before real verification", async () => {
  const env = fakeEnv({ "settings-v2": DEFAULT_SETTINGS });
  const cookie = await browserSession(env);

  const realMode = structuredClone(DEFAULT_SETTINGS);
  realMode.sync.dry_run = false;
  const realResponse = await putSettings(env, realMode, cookie);
  assert.equal(realResponse.status, 409);

  const enabled = structuredClone(DEFAULT_SETTINGS);
  enabled.enabled = true;
  const enabledResponse = await putSettings(env, enabled, cookie);
  assert.equal(enabledResponse.status, 409);
});

test("activation gate preserves authentication semantics for unsafe writes", async () => {
  const env = fakeEnv({ "settings-v2": DEFAULT_SETTINGS });
  const unsafe = structuredClone(DEFAULT_SETTINGS);
  unsafe.sync.dry_run = false;
  const response = await guardedWorker.fetch(new Request("https://example.test/api/admin/settings", {
    method: "PUT",
    headers: { Origin: "https://example.test", "Content-Type": "application/json" },
    body: JSON.stringify(unsafe),
  }), env);
  assert.equal(response.status, 401);
});

test("setup Basic auth may temporarily run real mode but cannot enable before verification", async () => {
  const env = fakeEnv({ "settings-v2": DEFAULT_SETTINGS });
  const authorization = basic(env);

  const realMode = structuredClone(DEFAULT_SETTINGS);
  realMode.sync.dry_run = false;
  const realResponse = await putSettings(env, realMode, authorization);
  assert.equal(realResponse.status, 200);

  const enabled = structuredClone(realMode);
  enabled.enabled = true;
  const enabledResponse = await putSettings(env, enabled, authorization);
  assert.equal(enabledResponse.status, 409);
});

test("only an accepted successful non-dry report with an upload verifies the real run", () => {
  assert.equal(successfulRealReport({ ok: true, dry_run: true, outcome: "success", stats: { uploaded: 1, failed: 0 } }), false);
  assert.equal(successfulRealReport({ ok: true, dry_run: false, outcome: "success", stats: { uploaded: 0, failed: 0 } }), false);
  assert.equal(successfulRealReport({ ok: true, dry_run: false, outcome: "partial", stats: { uploaded: 1, failed: 0 } }), true);
  assert.equal(successfulRealReport({ ok: true, dry_run: false, outcome: "success", stats: { uploaded: 1, failed: 1 } }), false);
  assert.equal(successfulRealReport({ dry_run: false, outcome: "success", stats: { uploaded: 1, failed: 0 } }), false);
  assert.equal(successfulRealReport({ ok: false, dry_run: false, outcome: "success", stats: { uploaded: 1, failed: 0 } }), false);
  assert.equal(successfulRealReport({ ok: true, dry_run: false, outcome: "unexpected", stats: { uploaded: 1, failed: 0 } }), false);
  assert.equal(successfulRealReport({ ok: true, dry_run: false, outcome: "success", stats: [] }), false);
});

test("agent report unlocks verification but not automatic scheduling", async () => {
  const env = fakeEnv({
    "settings-v2": { ...structuredClone(DEFAULT_SETTINGS), sync: { ...structuredClone(DEFAULT_SETTINGS.sync), dry_run: false } },
    "activation-v1": { version: 1, real_run_verified: false, enabled: false },
  });
  const response = await guardedWorker.fetch(new Request("https://example.test/api/report", {
    method: "POST",
    headers: { Authorization: `Bearer ${env.SYNC_API_TOKEN}`, "Content-Type": "application/json" },
    body: JSON.stringify({
      ok: true,
      dry_run: false,
      outcome: "partial",
      stats: { checked: 8, uploaded: 5, unchanged: 0, deferred: 3, failed: 0 },
    }),
  }), env);
  assert.equal(response.status, 200);
  const state = await activationState(env);
  assert.equal(state.real_run_verified, true);
  assert.equal(state.enabled, false);
});

test("zero-upload real report keeps automation locked", async () => {
  const env = fakeEnv({
    "settings-v2": { ...structuredClone(DEFAULT_SETTINGS), sync: { ...structuredClone(DEFAULT_SETTINGS.sync), dry_run: false } },
    "activation-v1": { version: 1, real_run_verified: false, enabled: false },
  });
  const response = await guardedWorker.fetch(new Request("https://example.test/api/report", {
    method: "POST",
    headers: { Authorization: `Bearer ${env.SYNC_API_TOKEN}`, "Content-Type": "application/json" },
    body: JSON.stringify({ ok: true, dry_run: false, outcome: "success", stats: { checked: 4, uploaded: 0, unchanged: 4, failed: 0 } }),
  }), env);
  assert.equal(response.status, 200);
  const state = await activationState(env);
  assert.equal(state.real_run_verified, false);
  assert.equal(state.enabled, false);
});

test("malformed successful-looking report cannot unlock verification", async () => {
  const env = fakeEnv({
    "settings-v2": { ...structuredClone(DEFAULT_SETTINGS), sync: { ...structuredClone(DEFAULT_SETTINGS.sync), dry_run: false } },
    "activation-v1": { version: 1, real_run_verified: false, enabled: false },
  });
  const response = await guardedWorker.fetch(new Request("https://example.test/api/report", {
    method: "POST",
    headers: { Authorization: `Bearer ${env.SYNC_API_TOKEN}`, "Content-Type": "application/json" },
    body: JSON.stringify({ ok: false, dry_run: false, outcome: "unexpected", stats: { checked: 1, uploaded: 1, failed: 0 } }),
  }), env);
  assert.equal(response.status, 200);
  const state = await activationState(env);
  assert.equal(state.real_run_verified, false);
  assert.equal(state.enabled, false);
});

test("verified Basic activation can enable normal dashboard operation", async () => {
  const env = fakeEnv({
    "settings-v2": DEFAULT_SETTINGS,
    "activation-v1": { version: 1, real_run_verified: true, enabled: false },
  });
  const enabled = structuredClone(DEFAULT_SETTINGS);
  enabled.enabled = true;
  enabled.sync.dry_run = false;
  const response = await putSettings(env, enabled, basic(env));
  assert.equal(response.status, 200);
  const state = await activationState(env);
  assert.equal(state.real_run_verified, true);
  assert.equal(state.enabled, true);
});
