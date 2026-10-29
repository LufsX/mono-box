import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import ts from "typescript";

const transpile = (source) => ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
}).outputText;
const load = (relative) => import(`data:text/javascript;base64,${Buffer.from(
  transpile(readFileSync(new URL(relative, import.meta.url), "utf8")),
).toString("base64")}`);
const modules = {
  "$lib/page-state/connections": await load("../src/lib/page-state/connections.ts"),
  "$lib/api/error-utils": await load("../src/lib/api/error-utils.ts"),
};
const source = readFileSync(new URL("../src/lib/pages/Connections.svelte", import.meta.url), "utf8");
const script = ts.createSourceFile("Connections.ts", source.match(/<script lang="ts">([\s\S]*?)<\/script>/)[1], ts.ScriptTarget.Latest, true);
const dependencies = {};
for (const statement of script.statements) {
  if (!ts.isImportDeclaration(statement)) continue;
  const exports = modules[statement.moduleSpecifier.text];
  if (!exports) continue;
  for (const binding of statement.importClause.namedBindings.elements) {
    dependencies[binding.name.text] = exports[binding.propertyName?.text ?? binding.name.text];
  }
}
// Execute the component's own functions with controlled requests and browser hooks.
const componentCode = script.statements.filter((statement) => !ts.isImportDeclaration(statement))
  .map((statement) => statement.getText(script)).join("\n");
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const flush = () => new Promise(setImmediate);
const fixture = () => ({
  connections: ["A", "B"].map((id) => ({
    id, metadata: { host: id, network: "tcp", type: "TUN" },
    upload: 100, download: 200, start: "2026-01-01T00:00:00Z", chains: ["DIRECT"],
  })),
  uploadTotal: 200, downloadTotal: 400,
});

function harness() {
  const reads = [], writes = [], timers = new Map();
  let mount, dispose, destroy, visibility, timerId = 0;
  const request = (list, args) => {
    const pending = { ...deferred(), args };
    list.push(pending);
    return pending.promise;
  };
  const document = {
    hidden: false,
    addEventListener(type, callback) { visibility = callback; },
    removeEventListener() { visibility = undefined; },
  };
  const deps = {
    ...dependencies,
    clashApi: {
      getConnections: () => request(reads),
      deleteConnection: (...args) => request(writes, args),
      closeAllConnections: (...args) => request(writes, args),
    },
    $state: (value) => value,
    onMount: (callback) => { mount = callback; },
    onDestroy: (callback) => { destroy = callback; },
    useModalHistory: (name, close) => ({ push() {}, close }),
    setTimeout(callback, delay) { const id = ++timerId; timers.set(id, { callback, delay }); return id; },
    clearTimeout(id) { timers.delete(id); },
    document,
  };
  const page = new Function(...Object.keys(deps), transpile(componentCode + `
    return {
      fetchConnections, closeConnection, closeAll, openDetail,
      snapshot: () => ({ connections, actionError, error, requestBusy, closingAll,
        closingConnectionIds, activeConnectionId }),
    };
  `))(...Object.values(deps));
  return {
    page, reads, writes, timers,
    async start() { dispose = mount(); reads[0].resolve(fixture()); await flush(); },
    visible(value) { document.hidden = !value; visibility?.(); },
    leave() { dispose(); destroy(); },
    tick() {
      assert.equal(timers.size, 1);
      const [id, timer] = timers.entries().next().value;
      timers.delete(id);
      timer.callback();
    },
  };
}
const actions = {
  single: (page) => page.closeConnection("A"),
  all: (page) => page.closeAll(),
};
function assertIdle(page) {
  assert.equal(page.snapshot().closingAll, false);
  assert.equal(page.snapshot().closingConnectionIds.size, 0);
}

for (const [kind, close] of Object.entries(actions)) {
  test(`${kind}: failed close restores polling and retains the list and action error`, async () => {
    const h = harness();
    await h.start();
    const closing = close(h.page);
    assert.equal(h.timers.size, 0);
    h.writes[0].reject(new Error("delete denied"));
    await closing;
    assertIdle(h.page);
    assert.match(h.page.snapshot().actionError, /delete denied/);
    assert.equal(h.page.snapshot().connections.length, 2);
    assert.equal(h.timers.size, 1);
    assert.equal([...h.timers.values()][0].delay, 1000);
    h.tick();
    assert.equal(h.reads.length, 2);
    h.reads[1].resolve(fixture());
    await flush();
    assert.equal(h.timers.size, 1);
    assert.match(h.page.snapshot().actionError, /delete denied/);
  });

  for (const refreshFails of [false, true]) {
    test(`${kind}: failed close leaves an in-flight refresh in charge, failure=${refreshFails}`, async () => {
      const h = harness();
      await h.start();
      h.tick();
      const closing = close(h.page);
      h.writes[0].reject(new Error("delete denied"));
      await closing;
      assert.equal(h.timers.size, 0);
      assert.equal(h.reads.length, 2);
      if (refreshFails) h.reads[1].reject(new Error("offline"));
      else h.reads[1].resolve(fixture());
      await flush();
      assert.equal(h.timers.size, 1);
      h.tick();
      assert.equal(h.reads.length, 3);
      h.reads[2].resolve(fixture());
      await flush();
    });
  }

  test(`${kind}: late close failure preserves the refresh's backoff timer`, async () => {
    const h = harness();
    await h.start();
    h.tick();
    h.reads[1].reject(new Error("offline"));
    await flush();
    h.tick();
    const closing = close(h.page);
    h.reads[2].reject(new Error("still offline"));
    await flush();
    const scheduled = [...h.timers.entries()];
    assert.equal(scheduled[0][1].delay, 2000);
    h.writes[0].reject(new Error("delete denied"));
    await closing;
    assert.deepEqual([...h.timers.entries()], scheduled);
  });

  test(`${kind}: hidden close failure waits for visibility before fetching again`, async () => {
    const h = harness();
    await h.start();
    const closing = close(h.page);
    h.visible(false);
    h.writes[0].reject(new Error("delete denied"));
    await closing;
    assert.equal(h.timers.size, 0);
    assert.equal(h.reads.length, 1);
    h.visible(true);
    assert.equal(h.reads.length, 2);
    h.reads[1].resolve(fixture());
    await flush();
    assert.equal(h.timers.size, 1);
  });

  for (const fails of [false, true]) {
    test(`${kind}: close settling after unmount starts no refresh or timer, failure=${fails}`, async () => {
      const h = harness();
      await h.start();
      h.page.openDetail("A");
      const closing = close(h.page);
      h.leave();
      if (fails) h.writes[0].reject(new Error("delete denied"));
      else h.writes[0].resolve();
      await closing;
      assertIdle(h.page);
      assert.equal(h.page.snapshot().activeConnectionId, "A");
      assert.equal(h.reads.length, 1);
      assert.equal(h.timers.size, 0);
    });
  }

  test(`${kind}: successful close with failed refresh retains its retry timer`, async () => {
    const h = harness();
    await h.start();
    const closing = close(h.page);
    h.writes[0].resolve();
    await flush();
    h.reads[1].reject(new Error("offline"));
    await closing;
    assertIdle(h.page);
    assert.match(h.page.snapshot().actionError, /连接已断开，但连接列表刷新失败/);
    assert.equal(h.timers.size, 1);
    assert.equal([...h.timers.values()][0].delay, 1000);
  });
}

test("concurrent single closes coalesce refreshes and await their own refresh generation", async () => {
  const h = harness();
  await h.start();
  h.tick();
  let firstDone = false, secondDone = false;
  const first = h.page.closeConnection("A").then(() => { firstDone = true; });
  h.writes[0].resolve();
  await flush();
  const second = h.page.closeConnection("B").then(() => { secondDone = true; });
  await h.page.closeConnection("B");
  assert.equal(h.writes.length, 2);
  h.reads[1].resolve(fixture());
  await flush();
  assert.equal(h.reads.length, 3);
  h.writes[1].resolve();
  await flush();
  h.reads[2].resolve(fixture());
  await flush();
  assert.equal(h.reads.length, 4);
  assert.equal(firstDone, true);
  assert.equal(secondDone, false);
  assert.equal(h.timers.size, 0);
  h.reads[3].resolve(fixture());
  await Promise.all([first, second]);
  assertIdle(h.page);
  assert.equal(h.timers.size, 1);
});

for (const fails of [false, true]) {
  test(`pending refresh settling after unmount does not apply data or restart polling, failure=${fails}`, async () => {
    const h = harness();
    await h.start();
    h.tick();
    const queued = h.page.fetchConnections(true);
    h.leave();
    if (fails) h.reads[1].reject(new Error("offline"));
    else h.reads[1].resolve({ connections: [] });
    await queued;
    await flush();
    assert.equal(h.page.snapshot().connections.length, 2);
    assert.equal(h.page.snapshot().error, "");
    assert.equal(h.reads.length, 2);
    assert.equal(h.timers.size, 0);
    assert.equal(h.page.snapshot().requestBusy, false);
  });
}
