import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, mkdtempSync, existsSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import ts from 'typescript';
import { runNativeShell } from './native-shell.mjs';

const moduleUrl = (source) => `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
const bridgeUrl = moduleUrl('export const runCommand = () => { throw new Error("unexpected bridge call"); };');
let { outputText } = ts.transpileModule(readFileSync(new URL('../src/lib/api/native.ts', import.meta.url), 'utf8'), {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
});
outputText = outputText.replaceAll('"../bridge"', JSON.stringify(bridgeUrl));
const { buildNativeCommand, toBase64Utf8 } = await import(moduleUrl(outputText));

test('active module selection follows Magisk Lite only when both binaries are executable', () => {
  const cases = [
    { standard: true, lite: false, version: '28.1', expected: 'standard' },
    { standard: true, lite: false, version: '28.1-lite', expected: 'standard' },
    { standard: false, lite: true, expected: 'lite' },
    { standard: false, lite: true, magisk: false, expected: 'lite' },
    { version: '28.1:MAGISK', expected: 'standard' },
    { version: '28.1:MAGISK-LITE', expected: 'lite' },
    { version: '28.1:magisk-lite', expected: 'lite' },
    { version: '28.1:Magisk-Lite', expected: 'lite' },
    { version: '', expected: 'standard' },
    { magisk: false, expected: 'standard' },
    { magiskExit: 1, version: '', expected: 'standard' },
    { standard: 'non-executable', expected: 'lite' },
    { lite: 'non-executable', version: 'lite', expected: 'standard' },
  ];
  const command = buildNativeCommand(['action', 'status']);
  assert.doesNotMatch(command, /modules_update|\/data\/adb\/box\/bin\/monobox|\bsu\b/);
  for (const { expected, ...options } of cases) {
    const result = runNativeShell(command, options);
    assert.equal(result.status, 0, JSON.stringify(options) + result.stderr);
    assert.equal(result.selected, result.paths[expected], JSON.stringify(options));
    assert.deepEqual(result.args, ['action', 'status']);
    assert.equal(result.stderr, '');
  }
});

test('missing active binaries return 127 without launching pending or legacy binaries', () => {
  for (const mode of [false, 'non-executable']) {
    const result = runNativeShell(buildNativeCommand(['module', 'info']), { standard: mode, lite: mode });
    assert.equal(result.status, 127);
    assert.equal(result.stdout, '');
    assert.equal(result.selected, undefined);
    assert.equal(result.stderr, 'Mono Box native binary not found in active module directories\n');
  }
});

test('native exit status and stderr pass through without selector output or retrying another binary', () => {
  const result = runNativeShell(buildNativeCommand(['action', 'start']), {
    version: 'Magisk Lite', binaryExit: 23, binaryStderr: 'native failed\n',
  });
  assert.equal(result.status, 23);
  assert.equal(result.stderr, 'native failed\n');
  assert.equal(result.selected, result.paths.lite);
  assert.deepEqual(result.args, ['action', 'start']);
});

test('shell arguments preserve literal quotes, substitutions, whitespace and UTF-8', (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'mono-box-injection-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const sentinel = join(directory, 'injected');
  const args = [
    '', '中文 专线', "O'Brien", 'a\\b"c', 'line one\nline two\tend',
    `$(/usr/bin/touch ${sentinel})`, `'; /usr/bin/touch ${sentinel}; #`,
    `\`/usr/bin/touch ${sentinel}\``, '$HOME', '*', '--help',
  ];
  for (const version of ['Magisk', 'Magisk Lite']) {
    const result = runNativeShell(buildNativeCommand(args), { version });
    assert.equal(result.status, 0, result.stderr);
    assert.deepEqual(result.args, args);
    assert.equal(existsSync(sentinel), false);
  }
  assert.equal(toBase64Utf8(args.join('\n')), Buffer.from(args.join('\n')).toString('base64'));
});

test('empty argument lists execute the selected binary without an extra empty argument', () => {
  const result = runNativeShell(buildNativeCommand([]));
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(result.args, []);
});
