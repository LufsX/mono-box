import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import ts from "typescript";

const { outputText } = ts.transpileModule(
  readFileSync(new URL("../src/lib/api/module-update.ts", import.meta.url), "utf8"),
  { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } },
);
const { MODULE_ABIS, getModuleAbi, getUpdateUrls, normalizeUpdateInfo, fetchUpdateInfo, fetchUpdateChangelog } = await import(
  `data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`,
);
const releases = "https://cors.isteed.cc/https://github.com/LufsX/mono-box/releases";
const installed = (abi) => `${releases}/download/Prerelease/update-${abi}.json`;
const metadata = (abi) => ({
  version: "v6.6",
  versionCode: 202610300001,
  zipUrl: `${releases}/download/v6.6/MonoBox_v6.6_${abi}.zip`,
  changelog: "Changes",
});
const response = (body, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});

test("all installed ABIs keep their architecture when switching release channels", () => {
  for (const abi of MODULE_ABIS) {
    const current = installed(abi);
    assert.equal(getModuleAbi(current + "?cache=1#fragment"), abi);
    assert.deepEqual(getUpdateUrls("current", current), [current]);
    for (const [source, path] of [["release", "latest/download"], ["nightly", "download/Prerelease"]]) {
      const expected = [`${releases}/${path}/update-${abi}.json`];
      if (abi === "arm64-v8a") expected.unshift(`${releases}/${path}/update.json`);
      assert.deepEqual(getUpdateUrls(source, current), expected);
    }
  }
});

test("installed architecture takes priority over the feed name and survives custom feeds", () => {
  for (const abi of MODULE_ABIS) {
    const custom = "https://updates.example.test/custom.json";
    assert.equal(getModuleAbi(custom, abi), abi);
    assert.deepEqual(getUpdateUrls("current", custom, abi), [custom]);
    const expected = [`${releases}/latest/download/update-${abi}.json`];
    if (abi === "arm64-v8a") expected.unshift(`${releases}/latest/download/update.json`);
    assert.deepEqual(getUpdateUrls("release", custom, abi), expected);
    assert.equal(getModuleAbi(installed("arm64-v8a"), abi), abi);
  }
});

test("legacy and missing metadata use the arm64 compatibility entry", () => {
  const legacy = `${releases}/latest/download/update.json`;
  assert.equal(getModuleAbi(legacy), "arm64-v8a");
  assert.equal(getModuleAbi(""), "arm64-v8a");
  assert.deepEqual(getUpdateUrls("current", legacy), [legacy]);
  assert.equal(getUpdateUrls("release", legacy)[0], legacy);
  assert.equal(getUpdateUrls("nightly", legacy)[0], `${releases}/download/Prerelease/update.json`);
  assert.equal(getUpdateUrls("current", "")[0], legacy);
});

test("current custom feeds stay intact, and GitHub forks retain repository and proxy", () => {
  const custom = "https://updates.example.test/custom.json?key=example";
  assert.deepEqual(getUpdateUrls("current", " " + custom + " "), [custom]);
  for (const prefix of ["https://github.com", "https://proxy.example/https://github.com"]) {
    const base = `${prefix}/owner/fork/releases`;
    const current = `${base}/download/Prerelease/update-x86_64.json`;
    assert.deepEqual(getUpdateUrls("release", current), [`${base}/latest/download/update-x86_64.json`]);
    assert.deepEqual(getUpdateUrls("nightly", current), [current]);
  }
});

test("legacy package names and numeric version-code strings remain accepted", () => {
  const info = { ...metadata("arm64-v8a"), versionCode: "202610300001", zipUrl: `${releases}/download/v6.5/MonoBox_v6.5.zip` };
  assert.equal(normalizeUpdateInfo(info, "arm64-v8a").versionCode, 202610300001);
  for (const abi of MODULE_ABIS) assert.deepEqual(normalizeUpdateInfo(metadata(abi), abi), metadata(abi));
});

test("a known package ABI must match the installed module", () => {
  for (const abi of MODULE_ABIS) {
    const other = abi === "arm64-v8a" ? "x86" : "arm64-v8a";
    assert.throws(() => normalizeUpdateInfo(metadata(other), abi), /更新包架构/);
  }
});

test("invalid manifests and unusable download URLs cannot become download actions", () => {
  for (const raw of [null, [], true, {}, { ...metadata("x86"), zipUrl: "javascript:alert(1)" },
    ...[0, -1, 1.2, true, [10], Number.MAX_SAFE_INTEGER + 1].map((versionCode) => ({ ...metadata("x86"), versionCode }))]) {
    assert.throws(() => normalizeUpdateInfo(raw, "x86"));
  }
});

test("arm64 accepts the generic manifest and falls back to its ABI manifest only on 404", async (t) => {
  const urls = getUpdateUrls("nightly", installed("arm64-v8a"));
  const fetchMock = t.mock.method(globalThis, "fetch");
  fetchMock.mock.mockImplementation(async (url, options) => {
    assert.equal(options.cache, "no-store");
    assert.equal(url, urls[0]);
    return response(metadata("arm64-v8a"));
  });
  await fetchUpdateInfo(urls, "arm64-v8a");
  assert.equal(fetchMock.mock.callCount(), 1);
  fetchMock.mock.resetCalls();
  fetchMock.mock.mockImplementation(async (url) => url === urls[0] ? response({}, 404) : response(metadata("arm64-v8a")));
  await fetchUpdateInfo(urls, "arm64-v8a");
  assert.deepEqual(fetchMock.mock.calls.map((call) => call.arguments[0]), urls);
});

test("other ABIs never fall back to arm64 on a missing manifest", async (t) => {
  for (const abi of MODULE_ABIS.filter((abi) => abi !== "arm64-v8a")) {
    const urls = getUpdateUrls("release", installed(abi));
    const fetchMock = t.mock.method(globalThis, "fetch", async () => response({}, 404));
    await assert.rejects(fetchUpdateInfo(urls, abi), /HTTP 404/);
    assert.equal(fetchMock.mock.callCount(), 1);
    assert.equal(fetchMock.mock.calls[0].arguments[0], urls[0]);
    fetchMock.mock.restore();
  }
});

function mockClock(t) {
  let now = 0, nextId = 0;
  const pending = new Map();
  t.mock.method(globalThis, "setTimeout", (callback, delay) => {
    const id = ++nextId;
    pending.set(id, { callback, at: now + delay });
    return id;
  });
  t.mock.method(globalThis, "clearTimeout", (id) => pending.delete(id));
  return {
    get pendingCount() { return pending.size; },
    tick(ms) {
      now += ms;
      for (const [id, task] of pending) {
        if (task.at <= now) {
          pending.delete(id);
          task.callback();
        }
      }
    },
  };
}

test("a stalled manifest fetch expires after 15 seconds and cleans up its deadline", async (t) => {
  const clock = mockClock(t);
  let activeSignal;
  t.mock.method(globalThis, "fetch", (url, { signal }) => {
    activeSignal = signal;
    return new Promise((resolve, reject) => signal.addEventListener("abort", () => reject(signal.reason), { once: true }));
  });
  const rejected = assert.rejects(fetchUpdateInfo([installed("x86")], "x86"), /更新请求超时/);
  clock.tick(14999);
  assert.equal(activeSignal.aborted, false);
  clock.tick(1);
  await rejected;
  assert.equal(activeSignal.aborted, true);
  assert.equal(clock.pendingCount, 0);
});

test("manifest fallback and body consumption share one 15-second deadline", async (t) => {
  const clock = mockClock(t);
  const urls = getUpdateUrls("release", installed("arm64-v8a"));
  let reading, activeSignal, canceled = false;
  const bodyStarted = new Promise((resolve) => { reading = resolve; });
  t.mock.method(globalThis, "fetch", async (url, { signal }) => {
    activeSignal = signal;
    if (url === urls[0]) {
      clock.tick(10000);
      return new Response(new ReadableStream({ cancel() { canceled = true; } }), { status: 404 });
    }
    const body = new Response(new ReadableStream({
      start(controller) {
        signal.addEventListener("abort", () => controller.error(new DOMException("Aborted", "AbortError")), { once: true });
      },
    }));
    const json = body.json.bind(body);
    t.mock.method(body, "json", () => { reading(); return json(); });
    return body;
  });
  const rejected = assert.rejects(fetchUpdateInfo(urls, "arm64-v8a"), /更新请求超时/);
  await bodyStarted;
  assert.equal(canceled, true);
  clock.tick(4999);
  assert.equal(activeSignal.aborted, false);
  clock.tick(1);
  await rejected;
  assert.equal(clock.pendingCount, 0);
});

test("a stalled changelog body expires and the same helper can retry successfully", async (t) => {
  const clock = mockClock(t);
  let reading;
  const started = new Promise((resolve) => { reading = resolve; });
  const fetchMock = t.mock.method(globalThis, "fetch", async (url, { signal }) => {
    const body = new Response(new ReadableStream({
      start(controller) {
        signal.addEventListener("abort", () => controller.error(new DOMException("Aborted", "AbortError")), { once: true });
      },
    }));
    const text = body.text.bind(body);
    t.mock.method(body, "text", () => { reading(); return text(); });
    return body;
  });
  const rejected = assert.rejects(fetchUpdateChangelog("https://updates.example/changelog.md"), /更新请求超时/);
  await started;
  clock.tick(15000);
  await rejected;
  assert.equal(clock.pendingCount, 0);
  fetchMock.mock.mockImplementation(async () => new Response("  Release notes\n"));
  assert.equal(await fetchUpdateChangelog("https://updates.example/changelog.md"), "Release notes");
  assert.equal(clock.pendingCount, 0);
});

test("caller cancellation aborts a pending request, removes listeners, and prevents pre-aborted fetches", async (t) => {
  const clock = mockClock(t);
  const owner = new AbortController();
  const removed = t.mock.method(owner.signal, "removeEventListener");
  let activeSignal;
  const fetchMock = t.mock.method(globalThis, "fetch", (url, { signal }) => {
    activeSignal = signal;
    return new Promise((resolve, reject) => signal.addEventListener("abort",
      () => reject(new DOMException("Aborted", "AbortError")), { once: true }));
  });
  const reason = new Error("Channel changed");
  const rejected = assert.rejects(fetchUpdateInfo([installed("x86")], "x86", owner.signal), (error) => error === reason);
  owner.abort(reason);
  await rejected;
  assert.equal(activeSignal.aborted, true);
  assert.equal(clock.pendingCount, 0);
  assert.equal(removed.mock.callCount(), 1);
  fetchMock.mock.resetCalls();
  await assert.rejects(fetchUpdateInfo([installed("x86")], "x86", owner.signal), (error) => error === reason);
  assert.equal(fetchMock.mock.callCount(), 0);
  assert.equal(clock.pendingCount, 0);
});

test("successful requests remove cancellation listeners and preserve unrelated network errors", async (t) => {
  const clock = mockClock(t);
  const owner = new AbortController();
  const removed = t.mock.method(owner.signal, "removeEventListener");
  let activeSignal;
  const fetchMock = t.mock.method(globalThis, "fetch", async (url, { signal }) => {
    activeSignal = signal;
    return response(metadata("x86"));
  });
  await fetchUpdateInfo([installed("x86")], "x86", owner.signal);
  assert.equal(clock.pendingCount, 0);
  assert.equal(removed.mock.callCount(), 1);
  owner.abort();
  clock.tick(15000);
  assert.equal(activeSignal.aborted, false);
  const failure = new TypeError("Connection reset");
  fetchMock.mock.mockImplementation(() => {
    const result = Promise.reject(failure);
    clock.tick(15000);
    return result;
  });
  await assert.rejects(fetchUpdateInfo([installed("x86")], "x86"), (error) => error === failure);
  assert.equal(clock.pendingCount, 0);
});

test("HTTP errors, malformed manifests and cross-ABI packages do not trigger fallback", async (t) => {
  const urls = getUpdateUrls("release", installed("arm64-v8a"));
  const fetchMock = t.mock.method(globalThis, "fetch");
  for (const makeResponse of [() => response({}, 500), () => response(null), () => response(metadata("x86"))]) {
    fetchMock.mock.resetCalls();
    fetchMock.mock.mockImplementation(async () => makeResponse());
    await assert.rejects(fetchUpdateInfo(urls, "arm64-v8a"));
    assert.equal(fetchMock.mock.callCount(), 1);
  }
  fetchMock.mock.resetCalls();
  fetchMock.mock.mockImplementation(async () => { throw new Error("network failure"); });
  await assert.rejects(fetchUpdateInfo(urls, "arm64-v8a"), /network failure/);
  assert.equal(fetchMock.mock.callCount(), 1);
});
