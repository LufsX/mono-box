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
  readBoxConfig: async () => 'clash_api_port=19090\nclash_api_secret="upgrade-test"\n',
});
const latest = 'update error: already using latest version alpha-smart-4624551';
const jsonResponse = (body, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { 'Content-Type': 'application/json' },
});

test('upgrade normalizes JSON success and empty 204, preserving readable messages', async (t) => {
  const responses = [
    [jsonResponse({ status: 'ok' }), { status: 'ok' }],
    [jsonResponse({ status: 'ok', message: 'Upgrade completed' }), { status: 'ok', message: 'Upgrade completed' }],
    [jsonResponse({}), { status: 'ok' }],
    [new Response(null, { status: 204 }), { status: 'ok' }],
  ];
  const fetchMock = t.mock.method(globalThis, 'fetch');
  for (const [response, expected] of responses) {
    fetchMock.mock.mockImplementation(async (url, options) => {
      assert.equal(url, 'http://127.0.0.1:19090/upgrade');
      assert.equal(options.method, 'POST');
      assert.equal(options.body, '{}');
      assert.equal(options.headers.Authorization, 'Bearer upgrade-test');
      return response;
    });
    assert.deepEqual(await api.upgradeCore(), expected);
  }
});

test('upgrade recognizes the complete known message in HTTP 500 and successful JSON', async (t) => {
  const fetchMock = t.mock.method(globalThis, 'fetch');
  for (const status of [500, 200]) {
    for (const version of ['alpha-smart-4624551', 'v1.19.21', 'v1.20.0-rc.1+build']) {
      const message = `update error: already using latest version ${version}`;
      fetchMock.mock.mockImplementation(async () => jsonResponse({ message }, status));
      assert.deepEqual(await api.upgradeCore(), { status: 'unchanged', message });
    }
  }
});

test('upgrade propagates real HTTP failures even when messages mention latest', async (t) => {
  const messages = [
    'update error: failed to download latest version',
    'update error: latest release metadata unavailable',
    'update error: permission denied',
    'already using latest version alpha-smart-4624551',
    'update error: already using latest version',
    `${latest}: failed to replace executable`,
    `${latest}\npermission denied`,
    `download failed: ${latest}`,
  ];
  const fetchMock = t.mock.method(globalThis, 'fetch');
  for (const message of messages) {
    fetchMock.mock.mockImplementation(async () => jsonResponse({ message }, 500));
    await assert.rejects(api.upgradeCore(), (error) => error.message === message);
  }
});

test('authentication and other HTTP statuses cannot become unchanged', async (t) => {
  const fetchMock = t.mock.method(globalThis, 'fetch');
  for (const status of [400, 401, 403, 404, 429, 502, 503]) {
    fetchMock.mock.mockImplementation(async () => jsonResponse({ message: latest }, status));
    await assert.rejects(api.upgradeCore(), (error) => error.status === status && error.message === latest);
  }
});

test('only a structured response message can identify an unchanged upgrade', async (t) => {
  const fetchMock = t.mock.method(globalThis, 'fetch');
  for (const body of [latest, JSON.stringify(latest), JSON.stringify({ error: latest }), '<html>latest</html>', '', '{invalid JSON']) {
    fetchMock.mock.mockImplementation(async () => new Response(body, { status: 500 }));
    await assert.rejects(api.upgradeCore(), /Clash API error: 500/);
  }
});

test('network and response-read errors propagate without matching their text', async (t) => {
  const fetchMock = t.mock.method(globalThis, 'fetch');
  for (const error of [new TypeError('Failed to fetch latest release'), new Error(latest)]) {
    fetchMock.mock.mockImplementation(async () => { throw error; });
    await assert.rejects(api.upgradeCore(), (received) => received === error);
    fetchMock.mock.mockImplementation(async () => ({
      ok: false, status: 500, text: async () => { throw error; },
    }));
    await assert.rejects(api.upgradeCore(), (received) => received === error);
  }
});

test('the same known message remains an error on other endpoints', async (t) => {
  const fetchMock = t.mock.method(globalThis, 'fetch', async () => jsonResponse({ message: latest }, 500));
  await assert.rejects(api.getConfigs(), (error) => error.message === latest);
  await assert.rejects(api.setTun(true), (error) => error.message === latest);
  assert.equal(fetchMock.mock.calls[0].arguments[0], 'http://127.0.0.1:19090/configs');
});

test('HTTP 200 does not conceal explicit failure status or malformed responses', async (t) => {
  const fetchMock = t.mock.method(globalThis, 'fetch');
  fetchMock.mock.mockImplementation(async () => jsonResponse({ status: 'error', message: 'Failed to download latest release' }));
  await assert.rejects(api.upgradeCore(), /Failed to download latest release/);
  for (const body of [[], 'latest release', 42, false]) {
    fetchMock.mock.mockImplementation(async () => jsonResponse(body));
    await assert.rejects(api.upgradeCore(), /Clash API 返回格式异常/);
  }
  fetchMock.mock.mockImplementation(async () => new Response('{invalid JSON', { status: 200 }));
  await assert.rejects(api.upgradeCore(), SyntaxError);
});
