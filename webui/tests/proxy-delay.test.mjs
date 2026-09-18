import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

async function load(relative, transform = (source) => source) {
  const source = transform(readFileSync(new URL(relative, import.meta.url), 'utf8'));
  const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}
const mapping = await load('../src/lib/page-state/proxies.ts');
const state = await load('../src/lib/page-state/proxy-testing.ts');
const providers = { subscription: { name: 'subscription', vehicleType: 'HTTP', proxies: [{ name: 'HK / 专线', type: 'SS' }] } };

test('provider-only targets use exact provider resource; ambiguous names do not guess', () => {
  assert.deepEqual(mapping.resolveProxyTestTarget({}, providers, 'HK / 专线'), { name: 'HK / 专线', apiName: 'HK / 专线', providerName: 'subscription', contextName: undefined });
  assert.equal(mapping.resolveProxyTestTarget({}, providers, 'HK/专线').apiName, undefined);
  assert.equal(mapping.resolveProxyTestTarget({}, { ...providers, second: { ...providers.subscription, name: 'second' } }, 'HK / 专线').apiName, undefined);
  assert.equal(mapping.resolveProxyTestTarget({ 'HK / 专线': {} }, providers, 'HK / 专线', undefined, 'subscription').providerName, 'subscription');
});

test('provider results and names with whitespace remain isolated', () => {
  let result = state.emptyProxyTestState();
  result = state.commitProxyTestResult(result, { name: 'A B', providerName: 'one' }, 45, {});
  assert.equal(state.readNodeTestLatency(result, 'A B', 'one'), 45);
  assert.equal(state.readNodeTestLatency(result, 'A B', 'two'), 0);
  assert.equal(state.readNodeTestLatency(result, 'AB', 'one'), 0);
  assert.equal(state.readNodeTestLatency(result, 'A B'), 0);
});

test('real API emits encoded provider healthcheck or direct delay routes', async () => {
  const commands = [];
  globalThis.__proxyTestExec = async (command) => { commands.push(command); return { errno: 0, stdout: '{"delay":42}', stderr: '' }; };
  const apiModule = await load('../src/lib/api/clash.ts', (source) => source
    .replace('import { exec } from "kernelsu";', 'const exec = (...args) => globalThis.__proxyTestExec(...args);')
    .replace(/import \{ loadStoredClashConfig, saveStoredClashConfig \} from .*?;/, 'const loadStoredClashConfig = () => ({ port: 9090, secret: "test" }); const saveStoredClashConfig = () => {};')
    .replace(/import \{ parseBoxConfig \} from .*?;/, 'const parseBoxConfig = () => ({});')
    .replace(/import \{ getErrorMessage, classifyConnectionError \} from .*?;/, 'const getErrorMessage = String; const classifyConnectionError = () => ({});'));
  const api = apiModule.createClashApi({});
  assert.equal(await api.testProxyDelay('HK / 专线', { providerName: '订阅/A', timeout: 99999 }), 42);
  assert.ok(commands[0].includes(`/providers/proxies/${encodeURIComponent('订阅/A')}/${encodeURIComponent('HK / 专线')}/healthcheck?`));
  assert.ok(commands[0].includes('timeout=32767'));
  assert.equal(await api.testProxyDelay('DIRECT'), 42);
  assert.ok(commands[1].includes('/proxies/DIRECT/delay?'));
  globalThis.__proxyTestExec = async () => ({ errno: 0, stdout: '{"message":"An error occurred in the delay test"}', stderr: '' });
  assert.equal(await api.testProxyDelay('DIRECT'), 0);
  globalThis.__proxyTestExec = async () => ({ errno: 0, stdout: '{"message":"Timeout"}', stderr: '' });
  assert.equal(await api.testProxyDelay('DIRECT'), 0);
  globalThis.__proxyTestExec = async () => ({ errno: 28, stdout: '', stderr: 'curl: operation timed out' });
  await assert.rejects(api.testProxyDelay('DIRECT'), /operation timed out/);
  globalThis.__proxyTestExec = async () => ({ errno: 0, stdout: '{"message":"Unauthorized"}', stderr: '' });
  await assert.rejects(api.testProxyDelay('DIRECT'), /Unauthorized/);
  delete globalThis.__proxyTestExec;
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
