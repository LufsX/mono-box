import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { writable, get } from "svelte/store";
import { parse } from "svelte/compiler";
import ts from "typescript";

const transpile = (source) => ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
}).outputText;
const load = (relative) => import(`data:text/javascript;base64,${Buffer.from(
  transpile(readFileSync(new URL(relative, import.meta.url), "utf8")),
).toString("base64")}`);
const modules = {
  "$lib/page-state/proxies": await load("../src/lib/page-state/proxies.ts"),
  "$lib/page-state/proxy-testing": await load("../src/lib/page-state/proxy-testing.ts"),
  "$lib/api/error-utils": await load("../src/lib/api/error-utils.ts"),
};
const source = readFileSync(new URL("../src/lib/pages/Proxies.svelte", import.meta.url), "utf8");
const script = ts.createSourceFile("Proxies.ts", source.match(/<script lang="ts">([\s\S]*?)<\/script>/)[1], ts.ScriptTarget.Latest, true);
const dependencies = {};
for (const statement of script.statements) {
  if (!ts.isImportDeclaration(statement)) continue;
  const exports = modules[statement.moduleSpecifier.text];
  if (!exports) continue;
  for (const binding of statement.importClause.namedBindings.elements) {
    dependencies[binding.name.text] = exports[binding.propertyName?.text ?? binding.name.text];
  }
}
// Execute the component's own declarations/functions; only imports, runes and browser hooks are supplied.
const componentCode = script.statements.filter((statement) => !ts.isImportDeclaration(statement))
  .map((statement) => statement.getText(script)).join("\n");
const elements = [];
function walk(node) {
  if (!node || typeof node !== "object") return;
  if (node.type === "Component" || node.type === "RegularElement") elements.push(node);
  for (const value of Object.values(node)) {
    if (Array.isArray(value)) value.forEach(walk);
    else if (value && typeof value === "object") walk(value);
  }
}
walk(parse(source, { modern: true }).fragment);
const attribute = (node, name) => node.attributes.find((attr) => attr.name === name);
function expression(node, name) {
  const expr = attribute(node, name).value.expression;
  return source.slice(expr.start, expr.end);
}
const modeControl = elements.find((node) => node.name === "Select");
const nodeControl = elements.find((node) => node.name === "ProxyNodeTile" && attribute(node, "selectable"));
const providerControl = elements.find((node) => node.name === "button"
  && attribute(node, "onclick") && expression(node, "onclick").includes("updateProvider("));
const testExports = `
return {
  loadData, switchMode, selectNode, updateProvider, testSingleNode, runNodeTest, runTestBatch,
  testGroup, testProvider, testProviderNode, readNodeLatency, providerUsableNodes, retryLoadData,
  resolveTestTarget, nodeFailed, isNodeTesting, providerTargets, providerCanTest, providerIsBusy,
  chooseMode(value) { modeSelectValue = value; return switchMode(value); },
  snapshot: () => ({ proxies, providers, modeSelectValue, currentMode: $currentMode,
    error, errorReason, switchingMode, selectingGroup, updatingProvider, mutationInFlight,
    testingOwners, testState, loading }),
  modeDisabled: () => Boolean(${expression(modeControl, "disabled")}),
  nodeSelectable: (group) => Boolean(${expression(nodeControl, "selectable")}),
  providerDisabled: (name) => Boolean(${expression(providerControl, "disabled")}),
};`;
const fixture = (mode = "global", now = "A") => ({
  proxies: {
    G: { name: "G", type: "Selector", now, all: ["A", "B", "C"] },
    ...Object.fromEntries(["A", "B", "C"].map((name) => [name, { name, type: "SS" }])),
  },
  providers: Object.fromEntries(["P", "Q"].map((name) => [name, { name, vehicleType: "HTTP", proxies: [] }])),
  configs: { mode },
});
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const flush = () => new Promise(setImmediate);
const event = { stopPropagation() {} };
const target = (name) => ({ name, apiName: name, contextName: "G" });

function harness() {
  const reads = [], writes = [], delays = [];
  const currentMode = writable("rule");
  let mount, dispose, poll, visibility;
  const write = (kind, args) => {
    const request = { kind, args, ...deferred() };
    writes.push(request);
    return request.promise;
  };
  const clashApi = {
    getProxies() {
      const parts = { proxies: deferred(), providers: deferred(), configs: deferred() };
      reads.push({
        parts,
        resolve(data = fixture()) { for (const [key, value] of Object.entries(parts)) value.resolve(data[key]); },
        reject(error) { parts.proxies.reject(error); parts.providers.resolve({}); parts.configs.resolve({}); },
      });
      return parts.proxies.promise;
    },
    getProxyProviders: () => reads.at(-1).parts.providers.promise,
    getConfigs: () => reads.at(-1).parts.configs.promise,
    setMode: (...args) => write("mode", args),
    setOutbound: (...args) => write("node", args),
    updateProxyProvider: (...args) => write("provider", args),
    testProxyDelay(...args) {
      const request = { args, ...deferred() };
      delays.push(request);
      return request.promise;
    },
  };
  const deps = {
    ...dependencies, clashApi, stores: { currentMode },
    $state: (value) => value,
    onMount: (callback) => { mount = callback; },
    loadHomeLayoutSettings: () => ({ proxyTestUrl: "" }),
    useModalHistory: () => ({ push() {}, close() {} }),
    setInterval: (callback) => { poll = callback; return 1; },
    clearInterval() { poll = undefined; },
    document: {
      hidden: false,
      addEventListener(type, callback) { visibility = callback; },
      removeEventListener() { visibility = undefined; },
    },
  };
  const page = new Function(...Object.keys(deps), `
    let $currentMode;
    stores.currentMode.subscribe(value => { $currentMode = value; });
    ${transpile(componentCode + testExports)}
  `)(...Object.values(deps));
  return {
    page, reads, writes, delays, currentMode,
    async start(data = fixture()) {
      dispose = mount();
      reads.at(-1).resolve(data);
      await flush();
    },
    leave() { dispose(); },
    poll() { poll?.(); visibility?.(); },
  };
}
function assertControls(page, busy) {
  assert.equal(page.modeDisabled(), busy);
  assert.equal(page.nodeSelectable({ type: "Selector" }), !busy);
  assert.equal(page.providerDisabled("Q"), busy);
}
const mutations = {
  mode: (page) => page.chooseMode("rule"),
  node: (page) => page.selectNode("G", "B"),
  provider: (page) => page.updateProvider(event, "P"),
};

test("initial config synchronizes the mode store, so global can switch to rule immediately", async () => {
  const h = harness();
  await h.start();
  assert.equal(get(h.currentMode), "global");
  assert.equal(h.page.snapshot().modeSelectValue, "global");
  await h.page.chooseMode("global");
  assert.equal(h.writes.length, 0);
  assert.equal(h.page.snapshot().modeSelectValue, "global");

  const operation = h.page.chooseMode("rule");
  assert.deepEqual(h.writes[0].args, ["rule"]);
  assert.equal(h.page.snapshot().modeSelectValue, "global");
  assertControls(h.page, true);
  h.writes[0].resolve();
  await flush();
  assert.equal(get(h.currentMode), "global");
  assertControls(h.page, true);
  // The core may return a different mode from the requested value.
  h.reads.at(-1).resolve(fixture("direct"));
  await operation;
  assert.equal(get(h.currentMode), "direct");
  assert.equal(h.page.snapshot().modeSelectValue, "direct");
  assertControls(h.page, false);
});

for (const kind of ["mode", "node"]) {
  for (const completion of ["during write", "after readback", "stale error"]) {
    test(`${kind}: old GET ${completion} cannot overwrite mutation state or errors`, async () => {
      const h = harness();
      await h.start();
      const oldLoad = h.page.loadData(true);
      const oldRead = h.reads.at(-1);
      const operation = mutations[kind](h.page);
      if (completion === "during write") {
        oldRead.resolve(fixture("direct", "C"));
        await oldLoad;
        assert.equal(h.page.snapshot().modeSelectValue, "global");
        assert.equal(h.page.snapshot().proxies.G.now, "A");
      }
      h.writes[0].resolve();
      await flush();
      assert.equal(h.page.snapshot().proxies.G.now, "A");
      assert.equal(h.page.snapshot().modeSelectValue, "global");
      // Node selection, too, must display what readback actually returns.
      h.reads.at(-1).resolve(fixture("rule", "C"));
      await operation;
      if (completion === "after readback") oldRead.resolve(fixture("global", "A"));
      if (completion === "stale error") oldRead.reject(new Error("obsolete GET"));
      await oldLoad;
      assert.equal(h.page.snapshot().modeSelectValue, "rule");
      assert.equal(get(h.currentMode), "rule");
      assert.equal(h.page.snapshot().proxies.G.now, "C");
      assert.equal(h.page.snapshot().error, "");
    });
  }
}

for (const [kind, mutate] of Object.entries(mutations)) {
  test(`${kind}: writes are exclusive, reads pause, and controls stay busy through readback`, async () => {
    const h = harness();
    await h.start();
    const operation = mutate(h.page);
    assertControls(h.page, true);
    await h.page.loadData();
    await h.page.loadData(true);
    h.poll();
    for (const attempt of Object.values(mutations)) await attempt(h.page);
    await h.page.updateProvider(event, "Q");
    assert.equal(h.writes.length, 1);
    assert.equal(h.reads.length, 1);
    assert.equal(h.page.snapshot().modeSelectValue, "global");

    h.writes[0].resolve();
    await flush();
    assert.equal(h.reads.length, 2);
    assertControls(h.page, true);
    for (const attempt of Object.values(mutations)) await attempt(h.page);
    assert.equal(h.writes.length, 1);
    h.reads[1].resolve(fixture("rule", "B"));
    await operation;
    assertControls(h.page, false);
    assert.equal(h.page.snapshot().mutationInFlight, false);
  });

  for (const refreshFails of [false, true]) {
    test(`${kind}: failed write retains error, resets controls, readback fails=${refreshFails}`, async () => {
      const h = harness();
      await h.start();
      const operation = mutate(h.page);
      h.writes[0].reject(new Error("write denied"));
      await flush();
      assertControls(h.page, true);
      assert.match(h.page.snapshot().error, /write denied/);
      assert.equal(h.page.snapshot().modeSelectValue, "global");
      if (refreshFails) h.reads[1].reject(new Error("refresh offline"));
      else h.reads[1].resolve();
      await operation;
      assert.match(h.page.snapshot().error, /write denied/);
      assert.equal(h.page.snapshot().error.includes("刷新代理信息失败:"), refreshFails);
      if (refreshFails) assert.match(h.page.snapshot().error, /refresh offline/);
      assert.equal(h.page.snapshot().errorReason, "");
      assert.equal(h.page.snapshot().modeSelectValue, "global");
      assert.equal(h.page.snapshot().proxies.G.now, "A");
      assertControls(h.page, false);
    });
  }
}

test("a successful write with failed readback keeps confirmed values and permits retry", async () => {
  const h = harness();
  await h.start();
  const operation = h.page.chooseMode("rule");
  h.writes[0].resolve();
  await flush();
  h.reads[1].reject(new Error("refresh offline"));
  await operation;
  assert.equal(h.page.snapshot().modeSelectValue, "global");
  assert.equal(get(h.currentMode), "global");
  assert.match(h.page.snapshot().error, /刷新代理信息失败:.*refresh offline/);
  assertControls(h.page, false);
  const retry = h.page.retryLoadData();
  h.reads[2].resolve(fixture("rule", "B"));
  await retry;
  assert.equal(h.page.snapshot().modeSelectValue, "rule");
  assert.equal(h.page.snapshot().error, "");
});

test("test completion during a mutation skips GET and mutation readback keeps its latency", async () => {
  const h = harness();
  await h.start();
  const testing = h.page.testSingleNode(event, "node:G:A", target("A"));
  const operation = h.page.chooseMode("rule");
  h.delays[0].resolve(42);
  await testing;
  assert.equal(h.reads.length, 1);
  assert.deepEqual(h.page.snapshot().testingOwners, {});
  h.writes[0].resolve();
  await flush();
  h.reads[1].resolve(fixture("rule", "B"));
  await operation;
  assert.equal(h.page.snapshot().proxies.G.now, "B");
  assert.equal(h.page.snapshot().testState.nodes['[null,"A"]'].delay, 42);
});

test("a refresh from one test preserves a concurrent test result committed after GET began", async () => {
  const h = harness();
  await h.start();
  const first = h.page.testSingleNode(event, "node:G:A", target("A"));
  // Exercise the actual test worker without starting another GET that would supersede the first.
  const second = h.page.runNodeTest(target("B"));
  await h.page.updateProvider(event, "P");
  assert.equal(h.writes.length, 0);
  h.delays[0].resolve(40);
  await flush();
  assert.equal(h.reads.length, 2);
  h.delays[1].resolve(0);
  await second;
  const snapshot = fixture();
  snapshot.proxies.B.history = [{ time: "2026-01-01T00:00:00Z", delay: 100 }];
  h.reads[1].resolve(snapshot);
  await first;
  assert.equal(h.reads.length, 2);
  assert.equal(h.page.snapshot().testState.nodes['[null,"B"]'].failed, true);
  assert.deepEqual(h.page.snapshot().testingOwners, {});
});

for (const kind of Object.keys(mutations)) {
  test(`${kind}: leaving during a write prevents final reads and ignores pending GET`, async () => {
    const h = harness();
    await h.start();
    const oldLoad = h.page.loadData(true);
    const operation = mutations[kind](h.page);
    h.leave();
    h.reads[1].resolve(fixture("direct", "C"));
    await oldLoad;
    h.writes[0].resolve();
    await operation;
    await h.page.loadData();
    h.poll();
    for (const attempt of Object.values(mutations)) await attempt(h.page);
    assert.equal(h.reads.length, 2);
    assert.equal(h.writes.length, 1);
    assert.equal(h.page.snapshot().modeSelectValue, "global");
    assert.equal(h.page.snapshot().proxies.G.now, "A");
    assert.equal(get(h.currentMode), "global");
  });
}

test("leaving during a test prevents its final refresh", async () => {
  const h = harness();
  await h.start();
  const testing = h.page.testSingleNode(event, "node:G:A", target("A"));
  h.leave();
  h.delays[0].resolve(42);
  await testing;
  assert.equal(h.reads.length, 1);
  assert.deepEqual(h.page.snapshot().testingOwners, {});
});

test("batch workers keep five requests in flight and commit every completed test", async () => {
  const h = harness();
  await h.start();
  const targets = Array.from({ length: 12 }, (_, i) => target(`N${i}`));
  const testing = h.page.runTestBatch("batch", targets);
  assert.equal(h.delays.length, 5);
  for (let i = 0; i < targets.length; i++) {
    assert.equal(h.delays.length, Math.min(targets.length, i + 5));
    h.delays[i].resolve(i + 1);
    await flush();
  }
  const results = await testing;
  assert.equal(results.length, 12);
  assert.equal(results.every((result) => result.status === "success"), true);
  assert.equal(h.page.snapshot().testState.revision, 12);
});

test("leaving a batch stops queued dispatch but lets the five issued requests settle", async () => {
  const h = harness();
  const data = fixture();
  const names = Array.from({ length: 12 }, (_, i) => `N${i}`);
  data.proxies.G.all = names;
  for (const name of names) data.proxies[name] = { name, type: "SS" };
  await h.start(data);
  const testing = h.page.testGroup(event, "G");
  assert.equal(h.delays.length, 5);
  h.delays[0].resolve(10);
  await flush();
  assert.equal(h.delays.length, 6);
  assert.equal(h.page.snapshot().testState.revision, 1);
  h.leave();
  for (const [i, request] of h.delays.slice(1).entries()) {
    if (i === 0) request.reject(new Error("issued request failed"));
    else request.resolve(42);
  }
  await testing;
  assert.equal(h.delays.length, 6);
  assert.equal(h.reads.length, 1);
  assert.equal(h.page.snapshot().testState.revision, 1);
  assert.deepEqual(h.page.snapshot().testingOwners, {});
  await h.page.runNodeTest(target("late"));
  assert.deepEqual(await h.page.runTestBatch("late", [target("later")]), []);
  assert.equal(h.delays.length, 6);
});

test("explicit provider latency display reads results without scanning provider membership", async () => {
  const h = harness();
  const data = fixture();
  const names = Array.from({ length: 1000 }, (_, i) => `N${i}`);
  let nameReads = 0;
  data.providers.P.proxies = names.map((name, i) => ({
    get name() { nameReads++; return name; },
    type: "SS",
    history: [{ time: "2026-01-01T00:00:00Z", delay: i % 2 ? 0 : 30 }],
  }));
  data.providers.Q.proxies = [{
    name: "N0", type: "SS", history: [{ time: "2026-01-01T00:00:00Z", delay: 90 }],
  }];
  data.proxies.N0 = {
    name: "N0", type: "SS", history: [{ time: "2026-01-01T00:00:00Z", delay: 10 }],
  };
  await h.start(data);
  nameReads = 0;
  for (const [i, name] of names.entries()) assert.equal(h.page.readNodeLatency(name, "P"), i % 2 ? 0 : 30);
  assert.equal(nameReads, 0);
  assert.equal(h.page.providerUsableNodes("P"), 500);
  assert.equal(nameReads, 1000);
  assert.equal(h.page.readNodeLatency("N0", "Q"), 90);
  assert.equal(h.page.readNodeLatency("N0"), 10);
  // An existing provider with a stale node name must not fall back to /proxies.
  await h.page.testProviderNode(event, "Q", "A");
  await h.page.testProviderNode(event, "removed", "N0");
  assert.equal(h.delays.length, 0);
  assert.match(h.page.snapshot().error, /无法确定节点的测速资源/);
});

for (const count of [1000, 2000]) {
  test(`provider row queries and batch target resolution reuse a linear index, nodes=${count}`, async () => {
    const h = harness();
    const data = fixture();
    const names = Array.from({ length: count }, (_, i) => `N${i}`);
    let nameReads = 0;
    data.providers.P.proxies = names.map((name, i) => ({
      get name() { nameReads++; return name; },
      type: "SS",
      history: [{ time: "2026-01-01T00:00:00Z", delay: i % 2 ? 0 : 30 }],
    }));
    await h.start(data);
    assert.ok(nameReads <= count * 8, `snapshot used ${nameReads} name reads`);
    nameReads = 0;
    for (const [i, name] of names.entries()) {
      assert.equal(h.page.resolveTestTarget(name, "P", "P").apiName, name);
      assert.equal(h.page.resolveTestTarget(name).providerName, "P");
      assert.equal(h.page.nodeFailed(name, "P"), Boolean(i % 2));
      assert.equal(h.page.isNodeTesting(name, "P"), false);
      assert.equal(h.page.readNodeLatency(name), i % 2 ? 0 : 30);
    }
    assert.equal(nameReads, 0);
    const targets = h.page.providerTargets("P");
    assert.equal(targets.length, count);
    assert.equal(targets.every((target) => target.providerName === "P" && target.apiName === target.name), true);
    assert.equal(nameReads, count);
    nameReads = 0;
    assert.equal(h.page.providerCanTest("P"), true);
    assert.equal(h.page.providerIsBusy("P"), false);
    assert.equal(nameReads, count * 2);
    nameReads = 0;
    const testing = h.page.testProvider(event, "P");
    assert.equal(nameReads, count);
    assert.equal(h.delays.length, 5);
    for (const [i, request] of h.delays.entries()) {
      assert.equal(request.args[0], names[i]);
      assert.equal(request.args[1].providerName, "P");
      assert.equal(h.page.isNodeTesting(names[i], "P"), true);
      assert.equal(h.page.isNodeTesting(names[i], "Q"), false);
    }
    assert.equal(nameReads, count);
    h.leave();
    for (const request of h.delays) request.resolve(30);
    await testing;
    assert.equal(h.delays.length, 5);
    assert.equal(h.reads.length, 1);
  });
}

test("provider refresh rebuilds membership and ambiguity even when syncRecord retains identity", async () => {
  const h = harness();
  const data = fixture();
  data.providers.P.proxies = [{ name: "shared", type: "SS" }, { name: "removed", type: "SS" }];
  await h.start(data);
  const originalProviders = h.page.snapshot().providers;
  assert.equal(h.page.resolveTestTarget("shared").providerName, "P");
  assert.equal(h.page.resolveTestTarget("removed", "P", "P").apiName, "removed");

  const refresh = h.page.loadData(true);
  const next = fixture();
  next.providers.P.proxies = [{ name: "shared", type: "SS" }, { name: "new", type: "SS" }];
  next.providers.Q.proxies = [{ name: "shared", type: "SS" }];
  next.proxies.removed = { name: "removed", type: "SS" };
  next.proxies.stale = { name: "stale", type: "SS", "provider-name": "deleted" };
  h.reads[1].resolve(next);
  await refresh;
  assert.equal(h.page.snapshot().providers, originalProviders);
  assert.equal(h.page.resolveTestTarget("shared").apiName, undefined);
  assert.equal(h.page.resolveTestTarget("shared", "P", "P").apiName, "shared");
  assert.equal(h.page.resolveTestTarget("new").providerName, "P");
  assert.equal(h.page.resolveTestTarget("removed", "P", "P").apiName, undefined);
  assert.equal(h.page.resolveTestTarget("stale").apiName, undefined);
  assert.equal(h.page.resolveTestTarget("removed", undefined, "deleted").apiName, undefined);
  await h.page.testProviderNode(event, "P", "removed");
  await h.page.testProviderNode(event, "deleted", "removed");
  assert.equal(h.delays.length, 0);

  const removal = h.page.loadData(true);
  const final = fixture();
  delete final.providers.P;
  final.providers.Q.proxies = [{ name: "shared", type: "SS" }];
  h.reads[2].resolve(final);
  await removal;
  assert.equal(h.page.resolveTestTarget("shared").providerName, "Q");
  assert.equal(h.page.resolveTestTarget("shared", "P", "P").apiName, undefined);
  assert.equal(h.page.resolveTestTarget("new").apiName, undefined);
});
