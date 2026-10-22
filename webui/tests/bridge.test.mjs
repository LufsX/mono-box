import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, mkdtempSync, rmSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import ts from 'typescript';

const moduleUrl = (source) => `data:text/javascript;base64,${Buffer.from(source).toString('base64')}`;
const kernelsuUrl = moduleUrl(readFileSync(new URL('../node_modules/kernelsu/index.js', import.meta.url), 'utf8'));

function typescriptUrl(relative, imports = {}) {
  const source = readFileSync(new URL(relative, import.meta.url), 'utf8');
  let { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
  for (const [specifier, url] of Object.entries(imports)) {
    outputText = outputText.replaceAll(JSON.stringify(specifier), JSON.stringify(url));
  }
  return moduleUrl(outputText);
}

const bridgeUrl = typescriptUrl('../src/lib/bridge.ts', { kernelsu: kernelsuUrl });
const { runCommand } = await import(bridgeUrl);
const actionUrl = typescriptUrl('../src/lib/api/action.ts', {
  kernelsu: kernelsuUrl,
  '../bridge': bridgeUrl,
  './config-parser': typescriptUrl('../src/lib/api/config-parser.ts'),
  './error-utils': typescriptUrl('../src/lib/api/error-utils.ts'),
});
const action = await import(actionUrl);

// Keep the official JS package; fake only the Android bridge's line events.
function captureSpawns(t) {
  const calls = [];
  globalThis.window = {};
  globalThis.ksu = window.ksu = {
    spawn(command, args, options, callback) {
      calls.push({ command, args: JSON.parse(args), options: JSON.parse(options), child: window[callback], callback });
    },
  };
  t.after(() => {
    delete globalThis.ksu;
    delete globalThis.window;
  });
  return calls;
}

function finish(call, stdout = [], stderr = [], errno = 0) {
  for (const line of stdout) call.child.stdout.emit('data', line);
  for (const line of stderr) call.child.stderr.emit('data', line);
  call.child.emit('exit', errno);
}

test('output streams before exit and retains newlines, blank lines and nonzero status', async (t) => {
  const calls = captureSpawns(t);
  const lines = [];
  let settled = false;
  const result = runCommand('command', {
    cwd: '/data/adb/box',
    env: { LANG: 'C.UTF-8' },
    onLine: (line, stream) => lines.push({ line, stream }),
  }).then((value) => { settled = true; return value; });
  const [call] = calls;
  assert.deepEqual(call.options, { cwd: '/data/adb/box', env: { LANG: 'C.UTF-8' } });
  assert.deepEqual(call.args, []);
  call.child.stdout.emit('data', 'first');
  call.child.stderr.emit('data', 'warning');
  call.child.stdout.emit('data', '');
  call.child.stdout.emit('data', '\u4e13\u7ebf');
  await Promise.resolve();
  assert.equal(settled, false);
  assert.deepEqual(lines, [
    { line: 'first', stream: 'stdout' },
    { line: 'warning', stream: 'stderr' },
    { line: '', stream: 'stdout' },
    { line: '\u4e13\u7ebf', stream: 'stdout' },
  ]);
  call.child.emit('exit', 7);
  assert.deepEqual(await result, { errno: 7, stdout: 'first\n\n\u4e13\u7ebf', stderr: 'warning' });
  assert.equal(window[call.callback], undefined);
});

test('concurrent commands keep their results isolated, including empty output', async (t) => {
  const calls = captureSpawns(t);
  const first = runCommand('first');
  const second = runCommand('second');
  finish(calls[1]);
  finish(calls[0], ['one', 'two']);
  assert.deepEqual(await first, { errno: 0, stdout: 'one\ntwo', stderr: '' });
  assert.deepEqual(await second, { errno: 0, stdout: '', stderr: '' });
});

test('spawn errors reject without turning ordinary nonzero exits into exceptions', async (t) => {
  const calls = captureSpawns(t);
  const result = runCommand('unavailable');
  const error = new Error('process could not be spawned');
  calls[0].child.emit('error', error);
  await assert.rejects(result, (caught) => caught === error);
  calls[0].child.emit('exit', -1);
});

test('missing native spawn rejects instead of leaving a pending command', async (t) => {
  captureSpawns(t);
  delete window.ksu.spawn;
  await assert.rejects(runCommand('command'), /KernelSU spawn bridge is unavailable/);
  delete globalThis.window;
  await assert.rejects(runCommand('command'), /KernelSU spawn bridge is unavailable/);
});

test('native trailing spaces cannot corrupt a heredoc terminator', async (t) => {
  const calls = captureSpawns(t);
  const result = runCommand("cat <<'EOF'\nfirst\n\nlast\nEOF");
  const output = spawnSync('/bin/sh', ['-c', `${calls[0].command} `], { encoding: 'utf8', timeout: 5000 });
  assert.equal(output.status, 0, output.stderr);
  assert.equal(output.stdout, 'first\n\nlast\n');
  finish(calls[0], output.stdout.slice(0, -1).split('\n'));
  assert.equal((await result).stdout, 'first\n\nlast');
});

test('module metadata and pretty-printed JSON survive line-based output', async (t) => {
  const calls = captureSpawns(t);
  const info = action.getModuleInfo();
  finish(calls[0], ['id=mono_box', 'name=Mono Box', 'version=v6.1', 'versionCode=123']);
  assert.equal((await info).versionCode, '123');

  const result = runCommand('curl');
  finish(calls[1], ['{', '  "delay": 42', '}']);
  assert.deepEqual(JSON.parse((await result).stdout), { delay: 42 });
});

test('box.config writes preserve normalized UTF-8 content through the nested heredoc', async (t) => {
  const calls = captureSpawns(t);
  const directory = mkdtempSync(join(tmpdir(), 'mono-box-bridge-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const path = join(directory, 'box.config');
  const content = 'clash_api_port=9090\r\n\r\n# \u4e13\u7ebf\r\n';
  const saved = action.writeBoxConfig(content);
  const command = calls[0].command.replace('/data/adb/box/scripts/box.config', path);
  const output = spawnSync('/bin/sh', ['-c', `su() { /bin/sh "$@"; }\n${command} `], { encoding: 'utf8', timeout: 5000 });
  assert.equal(output.status, 0, output.stderr);
  finish(calls[0]);
  await saved;
  assert.equal(readFileSync(path, 'utf8'), 'clash_api_port=9090\n\n# \u4e13\u7ebf\n');
});
