import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { get } from "svelte/store";
import ts from "typescript";

function moduleUrl(relative, imports = {}) {
  let { outputText } = ts.transpileModule(
    readFileSync(new URL(relative, import.meta.url), "utf8"),
    { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } },
  );
  for (const [specifier, url] of Object.entries(imports)) {
    outputText = outputText.replaceAll(JSON.stringify(specifier), JSON.stringify(url));
  }
  return `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`;
}
const { createClashStores } = await import(moduleUrl("../src/lib/api/clash-stores.ts", {
  "svelte/store": import.meta.resolve("svelte/store"),
  "./error-utils": moduleUrl("../src/lib/api/error-utils.ts"),
}));

test("one config request refreshes status, ports, TUN and proxy mode together", async () => {
  let calls = 0;
  const config = { mode: "global", "mixed-port": 7891, tun: { enable: true } };
  const { stores, actions } = createClashStores({
    async getConfigs() { calls++; return config; },
    async checkStatus() { throw new Error("redundant status request"); },
  });
  assert.deepEqual(await actions.refreshCoreStatus(), { ok: true });
  assert.equal(calls, 1);
  assert.equal(get(stores.configs), config);
  assert.equal(get(stores.currentMode), "global");
  assert.deepEqual(get(stores.coreStatus), { ok: true });
});

test("stop and restart clear stale config and recover with the current settings", async () => {
  let state = { mode: "global", "mixed-port": 7891, tun: { enable: true } };
  const { stores, actions } = createClashStores({
    async getConfigs() {
      if (state instanceof Error) throw state;
      return state;
    },
  });
  await actions.refreshConfigs();
  state = new Error("connection refused");
  assert.equal((await actions.refreshConfigs()).reason, "unreachable");
  assert.equal(get(stores.configs), null);
  assert.equal(get(stores.coreStatus).ok, false);
  state = { mode: "direct", "mixed-port": 7892, tun: { enable: false } };
  await actions.refreshConfigs();
  assert.equal(get(stores.configs)["mixed-port"], 7892);
  assert.equal(get(stores.configs).tun.enable, false);
  assert.equal(get(stores.currentMode), "direct");
  assert.deepEqual(get(stores.coreStatus), { ok: true });
});

test("late config success or failure cannot overwrite a newer refresh", async () => {
  for (const staleFails of [false, true]) {
    let complete;
    const stale = new Promise((resolve, reject) => { complete = staleFails ? reject : resolve; });
    let calls = 0;
    const config = { mode: "direct", tun: { enable: false } };
    const { stores, actions } = createClashStores({
      getConfigs() { return ++calls === 1 ? stale : Promise.resolve(config); },
    });
    const first = actions.refreshConfigs();
    await actions.refreshConfigs();
    complete(staleFails ? new Error("old request timed out") : { mode: "global", tun: { enable: true } });
    await first;
    assert.equal(get(stores.configs), config);
    assert.equal(get(stores.currentMode), "direct");
    assert.deepEqual(get(stores.coreStatus), { ok: true });
  }
});

test("late config success cannot hide a newer authentication failure", async () => {
  let complete;
  const stale = new Promise((resolve) => { complete = resolve; });
  let calls = 0;
  const { stores, actions } = createClashStores({
    getConfigs() { return ++calls === 1 ? stale : Promise.reject(new Error("HTTP 401 Unauthorized")); },
  });
  const first = actions.refreshConfigs();
  await actions.refreshConfigs();
  complete({ mode: "global" });
  await first;
  assert.equal(get(stores.configs), null);
  assert.equal(get(stores.coreStatus).reason, "unauthorized");
});

test("late version success or failure cannot replace the new core version", async () => {
  for (const staleFails of [false, true]) {
    let complete;
    const stale = new Promise((resolve, reject) => { complete = staleFails ? reject : resolve; });
    let calls = 0;
    const { stores, actions } = createClashStores({
      checkVersion() { return ++calls === 1 ? stale : Promise.resolve({ ok: true, version: "new" }); },
    });
    const first = actions.refreshVersion();
    await actions.refreshVersion();
    complete(staleFails ? new Error("old core stopped") : { ok: true, version: "old" });
    await first;
    assert.equal(get(stores.versionCheck).version, "new");
  }
});

test("authentication failures stay distinct from an unreachable core", async () => {
  const { stores, actions } = createClashStores({
    async getConfigs() { throw new Error("HTTP 401 Unauthorized"); },
  });
  const status = await actions.refreshConfigs();
  assert.equal(status.reason, "unauthorized");
  assert.equal(get(stores.configs), null);
  assert.equal(get(stores.coreStatus), status);
});
