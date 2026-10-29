import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const moduleUrl = (source) => `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
function typescriptUrl(relative, imports = {}) {
  const source = readFileSync(new URL(relative, import.meta.url), 'utf8');
  let { outputText } = ts.transpileModule(source, {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
  });
  for (const [specifier, url] of Object.entries(imports)) {
    outputText = outputText.replaceAll(JSON.stringify(specifier), JSON.stringify(url));
  }
  return moduleUrl(outputText);
}
const { createClashApi } = await import(typescriptUrl('../src/lib/api/clash.ts', {
  './native': moduleUrl('export function runNativeCommand() { throw new Error("unexpected native call"); } export const toBase64Utf8 = () => "";'),
  './clash-config-storage': typescriptUrl('../src/lib/api/clash-config-storage.ts'),
  './config-parser': typescriptUrl('../src/lib/api/config-parser.ts'),
  './error-utils': typescriptUrl('../src/lib/api/error-utils.ts'),
}));
const api = createClashApi({
  readBoxConfig: async () => 'clash_api_port=19090\nclash_api_secret=" timeout-test "\n',
});
const jsonResponse = (body, status = 200) => new Response(JSON.stringify(body), { status });
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
};

// Advance only API deadlines, without changing production budgets or waiting in real time.
function mockClock(t) {
  let now = 0, nextId = 0;
  const pending = new Map();
  t.mock.method(globalThis, 'setTimeout', (callback, delay) => {
    const id = ++nextId;
    pending.set(id, { callback, at: now + delay });
    return id;
  });
  const clear = t.mock.method(globalThis, 'clearTimeout', (id) => pending.delete(id));
  return {
    get pendingCount() { return pending.size; },
    get clearCount() { return clear.mock.callCount(); },
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

function pendingFetch(t) {
  const started = deferred();
  t.mock.method(globalThis, 'fetch', (url, { signal }) => {
    assert.ok(signal instanceof AbortSignal);
    const result = new Promise((resolve, reject) => {
      signal.addEventListener('abort', () => reject(signal.reason), { once: true });
    });
    started.resolve(signal);
    return result;
  });
  return started.promise;
}

function pendingBody(t, beforeHeaders) {
  const reading = deferred();
  t.mock.method(globalThis, 'fetch', async (url, { signal }) => {
    beforeHeaders?.();
    const response = new Response(new ReadableStream({
      start(controller) {
        // Fetch body consumption reports AbortError even with a custom abort reason.
        signal.addEventListener('abort', () => {
          controller.error(new DOMException('The operation was aborted.', 'AbortError'));
        }, { once: true });
      },
    }));
    const readText = response.text.bind(response);
    t.mock.method(response, 'text', () => {
      const result = readText();
      reading.resolve(signal);
      return result;
    });
    return response;
  });
  return reading.promise;
}

const isTimeout = (budget, path) => (error) => {
  assert.equal(error.message, `Clash API request timed out after ${budget} ms (GET ${path})`);
  assert.equal(error instanceof Error, true);
  return true;
};

test('pending Connections fetch aborts at 15 seconds, rejects, and allows another request', { timeout: 1000 }, async (t) => {
  const clock = mockClock(t);
  const started = pendingFetch(t);
  const rejected = assert.rejects(api.getConnections(), isTimeout(15000, '/connections'));
  const signal = await started;
  clock.tick(14999);
  assert.equal(signal.aborted, false);
  clock.tick(1);
  assert.equal(signal.aborted, true);
  await rejected;
  assert.equal(clock.pendingCount, 0);
  assert.equal(clock.clearCount, 1);

  t.mock.method(globalThis, 'fetch', async () => jsonResponse({ connections: [] }));
  assert.deepEqual(await api.getConnections(), { connections: [] });
  assert.equal(clock.pendingCount, 0);
});

test('pending Proxies response body shares the fetch deadline', { timeout: 1000 }, async (t) => {
  const clock = mockClock(t);
  // Headers take 10 seconds; reading the body must get only the remaining 5 seconds.
  const reading = pendingBody(t, () => clock.tick(10000));
  const rejected = assert.rejects(api.getProxies(), isTimeout(15000, '/proxies'));
  const signal = await reading;
  assert.equal(clock.pendingCount, 1);
  clock.tick(4999);
  assert.equal(signal.aborted, false);
  clock.tick(1);
  assert.equal(signal.aborted, true);
  await rejected;
  assert.equal(clock.pendingCount, 0);
  assert.equal(clock.clearCount, 1);

  t.mock.method(globalThis, 'fetch', async () => jsonResponse({ proxies: { DIRECT: { type: 'Direct' } } }));
  assert.deepEqual(await api.getProxies(), { DIRECT: { type: 'Direct' } });
  assert.equal(clock.pendingCount, 0);
});

test('successful JSON and empty responses clear their timers and preserve request configuration', async (t) => {
  const clock = mockClock(t);
  const signals = [];
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    signals.push(options.signal);
    if (url.endsWith('/version')) {
      assert.equal(url, 'http://127.0.0.1:29090/version');
      assert.equal(options.headers.Authorization, undefined);
      return jsonResponse({ version: 'v1.20.0', meta: true });
    }
    assert.equal(url, 'http://127.0.0.1:19090/configs');
    assert.equal(options.headers.Authorization, 'Bearer timeout-test');
    assert.equal(options.method, 'PATCH');
    assert.equal(options.body, '{"mode":"rule"}');
    return new Response(null, { status: 204 });
  });
  assert.deepEqual(await api.checkVersion({ port: 29090, secret: '' }), {
    ok: true, version: 'v1.20.0', meta: true,
  });
  assert.equal(clock.pendingCount, 0);
  await api.setMode('rule');
  assert.equal(clock.pendingCount, 0);
  assert.equal(clock.clearCount, 2);
  clock.tick(120000);
  assert.ok(signals.every((signal) => !signal.aborted));
});

test('HTTP and JSON parsing failures preserve their errors and clear timers', async (t) => {
  const clock = mockClock(t);
  const cases = [
    [() => jsonResponse({ message: 'Unauthorized' }, 401), (error) => error.status === 401 && error.message === 'Unauthorized'],
    [() => new Response('Bad gateway', { status: 502, statusText: 'Bad Gateway' }), /Clash API error: 502 Bad Gateway/],
    [() => new Response('{invalid JSON'), SyntaxError],
  ];
  const signals = [];
  const fetchMock = t.mock.method(globalThis, 'fetch');
  for (const [response, expected] of cases) {
    fetchMock.mock.mockImplementation(async (url, { signal }) => {
      signals.push(signal);
      return response();
    });
    await assert.rejects(api.getConnections(), expected);
    assert.equal(clock.pendingCount, 0);
  }
  assert.equal(clock.clearCount, cases.length);
  clock.tick(120000);
  assert.ok(signals.every((signal) => !signal.aborted));
});

test('network, body-read, and independent abort errors are not reported as timeouts', async (t) => {
  const clock = mockClock(t);
  const fetchMock = t.mock.method(globalThis, 'fetch');
  const signals = [];
  const errors = [
    new TypeError('Failed to fetch'),
    new DOMException('The operation was aborted by the user.', 'AbortError'),
    new Error('Response stream failed'),
  ];
  for (const error of errors) {
    for (const phase of ['fetch', 'body']) {
      fetchMock.mock.mockImplementation(async (url, { signal }) => {
        signals.push(signal);
        if (phase === 'fetch') throw error;
        return { text: async () => { throw error; } };
      });
      await assert.rejects(api.getConnections(), (received) => received === error);
      assert.equal(clock.pendingCount, 0);
    }
  }
  assert.equal(clock.clearCount, errors.length * 2);
  clock.tick(120000);
  assert.ok(signals.every((signal) => !signal.aborted));
});

test('an unrelated network failure is preserved even when the deadline fires before rejection is handled', async (t) => {
  const clock = mockClock(t);
  const failure = new TypeError('Connection reset');
  t.mock.method(globalThis, 'fetch', () => {
    const result = Promise.reject(failure);
    clock.tick(15000);
    return result;
  });
  await assert.rejects(api.getConnections(), (received) => received === failure);
  assert.equal(clock.pendingCount, 0);
  assert.equal(clock.clearCount, 1);
});

test('upgrade fetch has its own 120-second budget', { timeout: 1000 }, async (t) => {
  const clock = mockClock(t);
  const started = pendingFetch(t);
  const rejected = assert.rejects(api.upgradeCore(), {
    message: 'Clash API request timed out after 120000 ms (POST /upgrade)',
  });
  const signal = await started;
  clock.tick(15000);
  assert.equal(signal.aborted, false);
  clock.tick(104999);
  assert.equal(signal.aborted, false);
  clock.tick(1);
  assert.equal(signal.aborted, true);
  await rejected;
  assert.equal(clock.pendingCount, 0);
  assert.equal(clock.clearCount, 1);
});

test('upgrade response body also expires at the 120-second deadline', { timeout: 1000 }, async (t) => {
  const clock = mockClock(t);
  const reading = pendingBody(t, () => clock.tick(100000));
  const rejected = assert.rejects(api.upgradeCore(), {
    message: 'Clash API request timed out after 120000 ms (POST /upgrade)',
  });
  const signal = await reading;
  clock.tick(19999);
  assert.equal(signal.aborted, false);
  clock.tick(1);
  assert.equal(signal.aborted, true);
  await rejected;
  assert.equal(clock.pendingCount, 0);
  assert.equal(clock.clearCount, 1);
});

test('upgrade can finish after 15 seconds and clears the longer timer for success and already-latest responses', async (t) => {
  const clock = mockClock(t);
  const latest = 'update error: already using latest version v1.20.0';
  const cases = [
    [200, { status: 'ok' }, { status: 'ok' }],
    [200, { message: latest }, { status: 'unchanged', message: latest }],
    [500, { message: latest }, { status: 'unchanged', message: latest }],
  ];
  const signals = [];
  const fetchMock = t.mock.method(globalThis, 'fetch');
  for (const [status, body, expected] of cases) {
    fetchMock.mock.mockImplementation(async (url, { signal }) => {
      signals.push(signal);
      clock.tick(30000);
      assert.equal(signal.aborted, false);
      return jsonResponse(body, status);
    });
    assert.deepEqual(await api.upgradeCore(), expected);
    assert.equal(clock.pendingCount, 0);
  }
  assert.equal(clock.clearCount, cases.length);
  clock.tick(120000);
  assert.ok(signals.every((signal) => !signal.aborted));
});
