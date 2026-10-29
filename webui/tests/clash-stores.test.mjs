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

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const flush = () => new Promise(setImmediate);
const modeSource = readFileSync(new URL("../src/lib/components/home/ProxyMode.svelte", import.meta.url), "utf8");
const modeScript = ts.createSourceFile("ProxyMode.ts",
  modeSource.match(/<script lang="ts">([\s\S]*?)<\/script>/)[1], ts.ScriptTarget.Latest, true);
const modeCode = ts.transpileModule(
  modeScript.statements.filter((statement) => !ts.isImportDeclaration(statement))
    .map((statement) => statement.getText(modeScript)).join("\n"),
  { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } },
).outputText;

function modeHarness(disabled = false) {
  const writes = [], reads = [], logs = [];
  const { stores, actions } = createClashStores({
    setMode(mode) { const request = { mode, ...deferred() }; writes.push(request); return request.promise; },
    getConfigs() { const request = deferred(); reads.push(request); return request.promise; },
  });
  let destroy;
  const deps = {
    stores, actions,
    $state: (value) => value,
    $props: () => ({ disabled, onLog: (message, type) => logs.push({ message, type }) }),
    onDestroy: (callback) => { destroy = callback; },
  };
  const component = new Function(...Object.keys(deps), `
    let $currentMode;
    stores.currentMode.subscribe(value => { $currentMode = value; });
    ${modeCode}
    return { handleSwitch, busy: () => switchingMode };
  `)(...Object.values(deps));
  return { stores, actions, writes, reads, logs, component, leave: () => destroy() };
}

test("mode PATCH completion leaves the confirmed mode untouched and starts no GET", async () => {
  const h = modeHarness();
  const operation = h.actions.switchClashMode("global");
  assert.equal(h.writes[0].mode, "global");
  h.writes[0].resolve();
  await operation;
  assert.equal(get(h.stores.currentMode), "rule");
  assert.equal(h.reads.length, 0);
});

test("home mode stays busy until actual config readback and uses the returned mode", async () => {
  const h = modeHarness();
  await h.component.handleSwitch("rule");
  assert.equal(h.writes.length, 0);
  const operation = h.component.handleSwitch("global");
  assert.equal(h.component.busy(), "global");
  await h.component.handleSwitch("direct");
  assert.equal(h.writes.length, 1);
  h.writes[0].resolve();
  await flush();
  assert.equal(h.reads.length, 1);
  assert.equal(h.component.busy(), "global");
  assert.equal(get(h.stores.currentMode), "rule");
  await h.component.handleSwitch("direct");
  assert.equal(h.writes.length, 1);
  h.reads[0].resolve({ mode: "direct" });
  await operation;
  assert.equal(h.component.busy(), null);
  assert.equal(get(h.stores.currentMode), "direct");
  assert.equal(h.logs.at(-1).type, "success");
});

for (const failure of ["PATCH", "readback"]) {
  test(`home mode ${failure} failure notifies the parent and releases busy`, async () => {
    const h = modeHarness();
    const operation = h.component.handleSwitch("global");
    if (failure === "PATCH") h.writes[0].reject(new Error("write denied"));
    else {
      h.writes[0].resolve();
      await flush();
      h.reads[0].reject(new Error("readback offline"));
    }
    await operation;
    assert.equal(h.component.busy(), null);
    assert.equal(get(h.stores.currentMode), "rule");
    assert.equal(h.logs.at(-1).type, "error");
    assert.match(h.logs.at(-1).message, failure === "PATCH" ? /write denied/ : /readback offline/);
    assert.equal(h.reads.length, failure === "PATCH" ? 0 : 1);
  });
}

for (const readbackDone of [false, true]) {
  test(`destroyed home late PATCH cannot overwrite or supersede newer proxy config; readback done=${readbackDone}`, async () => {
    const h = modeHarness();
    const oldSwitch = h.component.handleSwitch("global");
    h.leave();
    const newSwitch = h.actions.switchClashMode("direct");
    h.writes[1].resolve();
    await newSwitch;
    const newReadback = h.actions.refreshConfigs();
    if (readbackDone) {
      h.reads[0].resolve({ mode: "direct" });
      await newReadback;
    }
    h.writes[0].resolve();
    await oldSwitch;
    assert.equal(h.component.busy(), null);
    assert.equal(h.reads.length, 1);
    if (!readbackDone) {
      h.reads[0].resolve({ mode: "direct" });
      await newReadback;
    }
    assert.equal(get(h.stores.currentMode), "direct");
    assert.equal(get(h.stores.configs).mode, "direct");
    await h.component.handleSwitch("global");
    assert.equal(h.writes.length, 2);
  });
}

test("destroyed home still reports a pending PATCH failure and releases busy without readback", async () => {
  const h = modeHarness();
  const operation = h.component.handleSwitch("global");
  h.leave();
  h.writes[0].reject(new Error("write denied"));
  await operation;
  assert.equal(h.component.busy(), null);
  assert.equal(h.reads.length, 0);
  assert.deepEqual(h.logs.at(-1), { message: "write denied", type: "error" });
});

for (const staleFails of [false, true]) {
  test(`destroyed home in-flight config GET cannot submit success or failure after proxies set direct; fails=${staleFails}`, async () => {
    const h = modeHarness();
    const initialConfig = { mode: "rule", "mixed-port": 7891 };
    const initialRefresh = h.actions.refreshConfigs();
    h.reads[0].resolve(initialConfig);
    await initialRefresh;
    const initialStatus = get(h.stores.coreStatus);
    const operation = h.component.handleSwitch("global");
    h.writes[0].resolve();
    await flush();
    assert.equal(h.reads.length, 2);
    assert.equal(h.component.busy(), "global");
    h.leave();
    // Proxies.loadData writes currentMode directly, without advancing configSequence.
    h.stores.currentMode.set("direct");
    if (staleFails) h.reads[1].reject(new Error("HTTP 401 Unauthorized"));
    else h.reads[1].resolve({ mode: "global", "mixed-port": 7892 });
    await operation;
    assert.equal(get(h.stores.currentMode), "direct");
    assert.equal(get(h.stores.configs), initialConfig);
    assert.equal(get(h.stores.coreStatus), initialStatus);
    assert.equal(h.component.busy(), null);
    assert.equal(h.reads.length, 2);
    assert.equal(h.logs.some((entry) => entry.type === "success"), false);
  });
}
