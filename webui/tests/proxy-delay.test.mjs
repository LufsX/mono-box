import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { runNativeShell } from './native-shell.mjs';

const moduleUrl = (source) => `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
function typescriptUrl(relative, transform = (source) => source, imports = {}) {
  const source = transform(readFileSync(new URL(relative, import.meta.url), 'utf8'));
  let { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
  for (const [specifier, url] of Object.entries(imports)) {
    outputText = outputText.replaceAll(JSON.stringify(specifier), JSON.stringify(url));
  }
  return moduleUrl(outputText);
}
const load = async (...args) => import(typescriptUrl(...args));
const nativeUrl = typescriptUrl('../src/lib/api/native.ts', undefined, {
  '../bridge': moduleUrl('export const runCommand = (...args) => globalThis.__proxyTestExec(...args);'),
});
const loadClashApi = () => load('../src/lib/api/clash.ts', (source) => source
  .replace(/import \{ loadStoredClashConfig, saveStoredClashConfig \} from .*?;/, 'const loadStoredClashConfig = () => ({ port: 9090, secret: "test" }); const saveStoredClashConfig = () => {};')
  .replace(/import \{ parseBoxConfig \} from .*?;/, 'const parseBoxConfig = () => ({});')
  .replace(/import \{ getErrorMessage, classifyConnectionError \} from .*?;/, 'const getErrorMessage = String; const classifyConnectionError = () => ({});'),
  { './native': nativeUrl });
const mapping = await load('../src/lib/page-state/proxies.ts');
const state = await load('../src/lib/page-state/proxy-testing.ts');
const providers = { subscription: { name: 'subscription', vehicleType: 'HTTP', proxies: [{ name: 'HK / 专线', type: 'SS' }] } };

test('provider-only targets use exact provider resource; ambiguous names do not guess', () => {
  assert.deepEqual(mapping.resolveProxyTestTarget({}, providers, 'HK / 专线'), { name: 'HK / 专线', apiName: 'HK / 专线', providerName: 'subscription', contextName: undefined });
  assert.equal(mapping.resolveProxyTestTarget({}, providers, 'HK/专线').apiName, undefined);
  assert.equal(mapping.resolveProxyTestTarget({}, { ...providers, second: { ...providers.subscription, name: 'second' } }, 'HK / 专线').apiName, undefined);
  assert.equal(mapping.resolveProxyTestTarget({ 'HK / 专线': {} }, providers, 'HK / 专线', undefined, 'subscription').providerName, 'subscription');
});

test('provider index preserves exact membership, duplicate ambiguity and compatible-provider rules', () => {
  const data = {
    P: { vehicleType: 'HTTP', proxies: [{ name: 'A B' }, { name: 'A B' }, { name: 'shared' }] },
    Q: { vehicleType: 'File', proxies: [{ name: 'shared' }] },
    C: { vehicleType: 'Compatible', proxies: [{ name: 'A B' }, { name: 'only-compatible' }] },
  };
  const index = mapping.buildProviderNodeIndex(data);
  const resolve = (name, proxies = {}, providerName) => mapping.resolveProxyTestTarget(proxies, data, name, 'context', providerName, index);
  assert.equal(resolve('A B').providerName, 'P');
  assert.equal(resolve('AB').apiName, undefined);
  assert.equal(resolve('shared').apiName, undefined);
  assert.equal(resolve('only-compatible').apiName, undefined);
  assert.equal(resolve('only-compatible', {}, 'C').apiName, 'only-compatible');
  assert.equal(resolve('shared', {}, 'Q').providerName, 'Q');
  assert.equal(resolve('shared', { shared: {} }).providerName, undefined);
  assert.equal(resolve('shared', { shared: {} }, 'deleted').apiName, undefined);
  assert.equal(resolve('shared', { shared: { 'provider-name': 'deleted' } }).apiName, undefined);
  assert.equal(resolve('A B', { 'A B': {} }, 'Q').apiName, undefined);
  for (const name of ['A B', 'AB', 'shared', 'only-compatible', 'missing']) {
    for (const providerName of [undefined, 'P', 'Q', 'C', 'deleted']) {
      assert.deepEqual(resolve(name, {}, providerName), mapping.resolveProxyTestTarget({}, data, name, 'context', providerName));
    }
  }
});

test('provider results and names with whitespace remain isolated', () => {
  let result = state.emptyProxyTestState();
  result = state.commitProxyTestResult(result, { name: 'A B', providerName: 'one' }, 45, {});
  assert.equal(state.readNodeTestLatency(result, 'A B', 'one'), 45);
  assert.equal(state.readNodeTestLatency(result, 'A B', 'two'), 0);
  assert.equal(state.readNodeTestLatency(result, 'AB', 'one'), 0);
  assert.equal(state.readNodeTestLatency(result, 'A B'), 0);
});

test('proxy delay sends typed UTF-8 requests to the native CLI and propagates failures', async (t) => {
  t.after(() => { delete globalThis.__proxyTestExec; });
  const commands = [];
  globalThis.__proxyTestExec = async (command, options) => {
    commands.push(command);
    assert.equal(options.cwd, '/data/adb/box');
    return { errno: 0, stdout: '{"delay":42}', stderr: '' };
  };
  const apiModule = await loadClashApi();
  const api = apiModule.createClashApi({});
  assert.equal(await api.testProxyDelay('HK / 专线', { providerName: '订阅/A', timeout: 99999, aliases: ['backup'] }), 42);
  const standard = runNativeShell(commands[0]);
  const lite = runNativeShell(commands[0], { version: '28.1-Magisk-Lite' });
  assert.equal(standard.status, 0, standard.stderr);
  assert.equal(standard.selected, standard.paths.standard);
  assert.equal(lite.status, 0, lite.stderr);
  assert.equal(lite.selected, lite.paths.lite);
  assert.deepEqual(lite.args, standard.args);
  assert.equal(standard.args[0], 'proxy-delay');
  assert.equal(standard.args.length, 2);
  const request = JSON.parse(Buffer.from(standard.args[1], 'base64').toString('utf8'));
  assert.deepEqual(request, { port: 9090, secret: 'test', name: 'HK / 专线', aliases: ['backup'], providerName: '订阅/A', timeout: 99999 });
  assert.equal(await api.testProxyDelay('DIRECT'), 42);
  globalThis.__proxyTestExec = async () => ({ errno: 0, stdout: '{"delay":0}', stderr: '' });
  assert.equal(await api.testProxyDelay('DIRECT'), 0);
  globalThis.__proxyTestExec = async () => ({ errno: 1, stdout: '', stderr: 'proxy delay request: operation timed out' });
  await assert.rejects(api.testProxyDelay('DIRECT'), /operation timed out/);
  globalThis.__proxyTestExec = async () => ({ errno: 1, stdout: '', stderr: 'Clash API HTTP 401: Unauthorized' });
  await assert.rejects(api.testProxyDelay('DIRECT'), /Unauthorized/);
  globalThis.__proxyTestExec = async () => ({ errno: 0, stdout: '{"delay":-1}', stderr: '' });
  await assert.rejects(api.testProxyDelay('DIRECT'), /有效延迟/);
  delete globalThis.__proxyTestExec;
});

test('large proxy requests share native chunk transport and preserve literal UTF-8 fields', async (t) => {
  t.after(() => { delete globalThis.__proxyTestExec; });
  const token = 'c'.repeat(32);
  const name = "中文 O'Brien $(id); " + 'N'.repeat(40000);
  const options = { aliases: ['"`$HOME'], providerName: "订阅/'; id", url: 'https://example.com/?q=$(id)&a="中文"' };
  const calls = [];
  const chunks = [];
  globalThis.__proxyTestExec = async (command) => {
    const result = runNativeShell(command, { version: 'Magisk Lite' });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.selected, result.paths.lite);
    const args = result.args;
    calls.push(args);
    assert.equal(args[0], 'request');
    if (args[1] === 'begin') return { errno: 0, stdout: token, stderr: '' };
    assert.equal(args[2], token);
    if (args[1] === 'append') {
      assert.ok(args[3].length <= 48 * 1024);
      chunks.push(args[3]);
    }
    return { errno: 0, stdout: args[1] === 'run' ? '{"delay":42}' : '', stderr: '' };
  };
  const api = (await loadClashApi()).createClashApi({});
  assert.equal(await api.testProxyDelay(name, options), 42);
  assert.ok(chunks.length > 1);
  assert.deepEqual(calls[0], ['request', 'begin']);
  assert.deepEqual(calls.at(-2), ['request', 'run', token]);
  assert.deepEqual(calls.at(-1), ['request', 'abort', token]);
  const request = JSON.parse(Buffer.from(chunks.join(''), 'base64').toString('utf8'));
  assert.equal(request[0], 'proxy-delay');
  assert.equal(request.length, 2);
  assert.deepEqual(JSON.parse(Buffer.from(request[1], 'base64').toString('utf8')), {
    port: 9090, secret: 'test', name, ...options, timeout: 5000,
  });
});

test('provider metadata is validated and explicit stale context never falls back', () => {
  const name = 'HK / 专线';
  assert.equal(mapping.resolveProxyTestTarget({ [name]: { 'provider-name': 'subscription' } }, providers, name).providerName, 'subscription');
  assert.equal(mapping.resolveProxyTestTarget({ [name]: {} }, providers, name, undefined, 'removed').apiName, undefined);
  assert.equal(mapping.resolveProxyTestTarget({ [name]: { 'provider-name': 'removed' } }, providers, name).apiName, undefined);
});

test('new core history replaces local results, stale history cannot, and removed nodes are pruned', () => {
  const node = (time, delay) => ({ name: 'N', type: 'SS', history: [{ time, delay }] });
  const old = { N: node('2026-01-01T00:00:00Z', 50) };
  let result = state.seedProxyTestState(state.emptyProxyTestState(), old, {});
  result = state.commitProxyTestResult(result, { name: 'N' }, 0);
  result = state.seedProxyTestState(result, old, {});
  assert.equal(state.isNodeTestFailed(result, 'N'), true);
  result = state.seedProxyTestState(result, { N: node('2026-01-01T00:00:01Z', 80) }, {});
  assert.equal(state.readNodeTestLatency(result, 'N'), 80);
  result = state.seedProxyTestState(result, { N: node('2026-01-01T00:00:02Z', 0) }, {});
  assert.equal(state.isNodeTestFailed(result, 'N'), true);
  result = state.seedProxyTestState(result, {}, {});
  assert.equal(state.readNodeTestResult(result, 'N'), undefined);
});

test('history refresh respects provider identity, extra history, and in-flight test revision', () => {
  const make = (delay, time) => ({ name: 'same', type: 'SS', history: [{ time, delay }] });
  const a = '2026-01-01T00:00:00Z', b = '2026-01-01T00:00:01Z';
  const provider = (proxy) => ({ name: 'P', vehicleType: 'HTTP', proxies: [proxy] });
  let result = state.seedProxyTestState(state.emptyProxyTestState(), { same: make(10, a) }, { P: provider(make(20, a)) });
  const revision = result.revision;
  result = state.commitProxyTestResult(result, { name: 'same', providerName: 'P' }, 0);
  const newer = { ...make(20, a), extra: { url: { history: [{ time: b, delay: 30 }] } } };
  result = state.seedProxyTestState(result, { same: make(10, a) }, { P: provider(newer) }, revision);
  assert.equal(state.isNodeTestFailed(result, 'same', 'P'), true);
  result = state.seedProxyTestState(result, { same: make(10, a) }, { P: provider(newer) });
  assert.equal(state.readNodeTestLatency(result, 'same', 'P'), 30);
  assert.equal(state.readNodeTestLatency(result, 'same'), 10);
  assert.notEqual(state.proxyTestKey('["P","same"]'), state.proxyTestKey('same', 'P'));
});

test('top-level details do not inherit provider metadata and whitespace stays distinct', () => {
  const details = mapping.buildProxyDetailMap({ 'A B': { name: 'A B', type: 'Direct' } }, {
    P: { vehicleType: 'HTTP', proxies: [{ name: 'A B', type: 'SS', 'provider-name': 'P' }, { name: 'AB', type: 'VLESS' }] },
  });
  assert.equal(details['A B']['provider-name'], undefined);
  assert.equal(details['A B'].type, 'Direct');
  assert.equal(details.AB.type, 'VLESS');
});
