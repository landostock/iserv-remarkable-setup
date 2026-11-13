import test from "node:test";
import assert from "node:assert/strict";
import { DASHBOARD_HTML } from "./dashboard.js";
import { DASHBOARD_JS } from "./dashboard_client.js";
import { DASHBOARD_ENHANCEMENT_JS } from "./dashboard_enhancement.js";

const REQUIRED_IDS = [
  "langDE", "langEN", "refresh", "logout",
  "enabled", "dryRun", "modePill", "activationNote", "status",
  "scheduleSummary", "interval", "dayTabs", "dayTitle", "dayHint",
  "addWindow", "copyTimes", "windows", "emptyWindows", "copyPanel",
  "copyDays", "applyCopy", "cancelCopy",
  "sources", "addSource", "typePdf", "typeEpub", "lookback", "maxFile",
  "actions", "save", "run", "message",
];

test("polished dashboard keeps every functional client hook", () => {
  for (const id of REQUIRED_IDS) {
    assert.match(DASHBOARD_HTML, new RegExp(`id=["']${id}["']`), `missing dashboard element #${id}`);
  }
});

test("dashboard enhancement provides actionable privacy-safe IServ failures", () => {
  for (const reason of ["iserv_auth", "iserv_source", "iserv_certificate", "iserv_storage", "iserv_network", "iserv_provider"]) {
    assert.match(DASHBOARD_ENHANCEMENT_JS, new RegExp(reason));
  }
  assert.doesNotMatch(DASHBOARD_ENHANCEMENT_JS, /report\.error/);
});

test("dashboard client remains dependency-free and uses the expected contract", () => {
  assert.doesNotMatch(DASHBOARD_HTML, /<script[^>]+src=["']https?:/i);
  assert.doesNotMatch(DASHBOARD_HTML, /<link[^>]+href=["']https?:/i);
  for (const id of ["enabled", "dryRun", "status", "dayTabs", "windows", "sources", "save", "run"]) {
    assert.match(DASHBOARD_JS, new RegExp(`#${id}`), `client no longer references expected #${id}`);
  }
});

test("last-run information preserves the requested two-row hierarchy", () => {
  assert.match(DASHBOARD_JS, /status-result/);
  assert.match(DASHBOARD_JS, /t\("state"\)/);
  assert.match(DASHBOARD_JS, /t\("last"\)/);
  assert.match(DASHBOARD_JS, /t\("result"\)/);
});
