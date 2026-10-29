import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, mkdtempSync, rmSync, existsSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import ts from 'typescript';
import { runNativeShell } from './native-shell.mjs';

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
const nativeUrl = typescriptUrl('../src/lib/api/native.ts', { '../bridge': bridgeUrl });
const actionUrl = typescriptUrl('../src/lib/api/action.ts', {
  './native': nativeUrl,
  kernelsu: kernelsuUrl,
  '../bridge': bridgeUrl,
  './config-parser': typescriptUrl('../src/lib/api/config-parser.ts'),
  './error-utils': typescriptUrl('../src/lib/api/error-utils.ts'),
});
const action = await import(actionUrl);
const native = await import(nativeUrl);

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

test('synchronous native spawn errors reject instead of leaving a pending command', async (t) => {
  captureSpawns(t);
  const error = new Error('native bridge rejected the command');
  window.ksu.spawn = () => { throw error; };
  await assert.rejects(runCommand('unavailable'), (caught) => caught === error);
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

test('exec and exit stay inside the command and preserve the libsu completion protocol', async (t) => {
  const calls = captureSpawns(t);
  for (const [command, expectedCode, expectedOutput] of [
    ["exec sh -c 'printf started; exit 0'", 0, 'started'],
    ["exec sh -c 'printf failed >&2; exit 7'", 7, ''],
    ['exit 3', 3, ''],
  ]) {
    const pending = runCommand(command);
    const call = calls.at(-1);
    // libsu runs this after the supplied command to retrieve its actual status.
    const shell = call.command + 'task_result=$?\nprintf "\\n__DONE__:%s\\n" "$task_result"\n';
    const output = spawnSync('/bin/sh', ['-c', shell], { encoding: 'utf8', timeout: 5000 });
    assert.equal(output.status, 0, output.stderr);
    assert.equal(output.stdout, expectedOutput + '\n__DONE__:' + expectedCode + '\n');
    finish(call, expectedOutput ? [expectedOutput] : [], output.stderr ? [output.stderr] : [], expectedCode);
    assert.equal((await pending).errno, expectedCode);
  }
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

test('box.config writes pass normalized UTF-8 base64 to the native CLI', async (t) => {
  const calls = captureSpawns(t);
  const content = 'clash_api_port=9090\r\n\r\n# \u4e13\u7ebf\r\n';
  const saved = action.writeBoxConfig(content);
  const expected = 'clash_api_port=9090\n\n# \u4e13\u7ebf\n';
  const encoded = Buffer.from(expected).toString('base64');
  assert.deepEqual(nativeArgs(calls[0]), ['config', 'write', encoded]);
  assert.deepEqual(calls[0].options, { cwd: '/data/adb/box' });
  assert.equal(Buffer.from(encoded, 'base64').toString('utf8'), expected);
  finish(calls[0]);
  await saved;
});

test('external URLs remain a single shell argument without command substitution', async (t) => {
  const calls = captureSpawns(t);
  const directory = mkdtempSync(join(tmpdir(), 'mono-box-url-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const sentinel = join(directory, 'injected');
  const url = `https://example.com/$(touch ${sentinel})`;
  const opened = action.openExternalUrlCommand(url);
  const shell = `am() { printf '%s' "$5"; }; ${calls[0].command} `;
  const output = spawnSync('/bin/sh', ['-c', shell], { encoding: 'utf8', timeout: 5000 });

  assert.equal(output.status, 0, output.stderr);
  assert.equal(output.stdout, url);
  assert.equal(existsSync(sentinel), false);
  finish(calls[0]);
  await opened;
});


function nativeArgs(call) {
  // Execute the full generated selector with temporary module binaries.
  const output = runNativeShell(call.command);
  assert.equal(output.status, 0, output.stderr);
  assert.equal(output.selected, output.paths.standard);
  assert.deepEqual(call.options, { cwd: '/data/adb/box' });
  return output.args;
}

const encodeText = (value) => Buffer.from(value).toString('base64');
const profileRow = (name, active = false, source = '') =>
  `profile\t${name}\t/data/adb/box/mihomo/.mono-box/profiles/${name}\t42\t1700000000\t${encodeText(source)}\t${active}`;

test('module/config reads and atomic config patches use native command families', async (t) => {
  const calls = captureSpawns(t);
  const info = action.getModuleInfo();
  assert.deepEqual(nativeArgs(calls[0]), ['module', 'info']);
  finish(calls[0], ['id=mono_box_lite', 'version=lite', 'versionCode=3']);
  assert.equal((await info).id, 'mono_box_lite');
  const config = action.configPortAdapter.readBoxConfig();
  assert.deepEqual(nativeArgs(calls[1]), ['config', 'read']);
  finish(calls[1], ['# 中文', "clash_api_secret='literal $()'", '']);
  assert.equal(await config, "# 中文\nclash_api_secret='literal $()'\n");
  const updates = { clash_api_secret: "'中文 $(literal) \\' \"'", clash_api_port: '9091' };
  const patched = action.configPortAdapter.updateBoxConfigValues(updates);
  const args = nativeArgs(calls[2]);
  assert.deepEqual(args.slice(0, 2), ['config', 'patch']);
  assert.deepEqual(JSON.parse(Buffer.from(args[2], 'base64').toString('utf8')), updates);
  assert.equal(args.length, 3);
  finish(calls[2]);
  await patched;
});

test('profile commands encode all arbitrary UTF-8 text and retain TSV parsing', async (t) => {
  const calls = captureSpawns(t);
  const rawName = "dir/中文 O'Brien $(literal).yaml";
  const name = action.normalizeMihomoConfigName(rawName);
  const source = "https://example.com/sub?token='&x=$(literal)";
  const content = '# 中文\r\nmode: rule\r\n';
  const cases = [
    { invoke: () => action.importMihomoConfigFile(rawName, content), args: ['import', encodeText(name), encodeText('# 中文\nmode: rule\n')] },
    { invoke: () => action.downloadMihomoConfigFromUrl(` ${source} `, rawName), args: ['download', encodeText(source), encodeText(name)] },
    { invoke: () => action.updateMihomoConfigFromUrl(rawName), args: ['update', encodeText(name)] },
    { invoke: () => action.switchMihomoConfigFile(rawName), args: ['switch', encodeText(name), 'profile'], active: true },
    { invoke: () => action.switchMihomoConfigFile('config.yaml', 'current'), args: ['switch', encodeText('config.yaml'), 'current'], current: true },
    { invoke: () => action.deleteMihomoConfigFile(rawName), args: ['delete', encodeText(name)], deleted: true },
  ];
  for (const entry of cases) {
    const result = entry.invoke();
    const call = calls.at(-1);
    assert.deepEqual(nativeArgs(call), ['profiles', ...entry.args]);
    finish(call, entry.deleted ? [] : [entry.current
      ? 'current\tconfig.yaml\t/data/adb/box/mihomo/config.yaml\t20\t1700000001\t\ttrue'
      : profileRow(name, entry.active, source)]);
    const file = await result;
    if (entry.deleted) assert.equal(file, undefined);
    else {
      assert.equal(file.name, entry.current ? 'config.yaml' : name);
      assert.equal(file.kind, entry.current ? 'current' : 'profile');
      assert.equal(file.active, !!entry.active || !!entry.current);
      assert.equal(file.sourceUrl, entry.current ? '' : source);
      assert.equal(file.updatedAt, (entry.current ? 1700000001 : 1700000000) * 1000);
    }
  }
  const listed = action.listMihomoConfigFiles();
  assert.deepEqual(nativeArgs(calls.at(-1)), ['profiles', 'list']);
  finish(calls.at(-1), [profileRow(name, true, source)]);
  assert.equal((await listed)[0].sourceUrl, source);
});

test('log read/open encode paths and log reports preserve totals', async (t) => {
  const calls = captureSpawns(t);
  const path = "/data/adb/box/mihomo/mihomo_中文'$(literal).log";
  const read = action.readLogFile(path);
  assert.deepEqual(nativeArgs(calls[0]), ['logs', 'read', encodeText(path)]);
  finish(calls[0], ['first', '', 'last']);
  assert.equal(await read, 'first\n\nlast');
  const opened = action.openLogFileLocation(path);
  assert.deepEqual(nativeArgs(calls[1]), ['logs', 'open', encodeText(path)]);
  finish(calls[1], ['opened\t/sdcard/Download/MonoBox/logs/example.log']);
  assert.equal((await opened).errno, 0);
  const report = action.getLogSizeReport();
  assert.deepEqual(nativeArgs(calls[2]), ['service', 'logs_size']);
  finish(calls[2], [`file\t42\t${path}`, 'total\t42\t1']);
  assert.deepEqual(await report, { files: [{ path, size: 42 }], totalBytes: 42, count: 1 });
  const cleared = action.clearLogFiles();
  assert.deepEqual(nativeArgs(calls[3]), ['service', 'clear_logs']);
  finish(calls[3]);
  await cleared;
});

test('action whitelist validates commands and enum arguments before spawning', async (t) => {
  const calls = captureSpawns(t);
  for (const command of ['', 'unknown', 'start extra', 'start; id', 'start\nstop', 'start\0', 'switch_mode',
    'switch_mode Rule', 'switch_mode rule extra', "switch_mode 'rule'", 'switch_mode $(id)', 'switch_tun 1',
    'switch_tun false;id', 'logs read anything', 'config read']) {
    await assert.rejects(action.runActionScript(command), /Invalid action/);
  }
  assert.equal(calls.length, 0);
  const cases = [
    ...['toggle', 'start', 'stop', 'restart', 'status', 'get_configs', 'upgrade_core', '--help', '-h']
      .map((name) => [name, ['action', name]]),
    [' switch_mode\trule ', ['action', 'switch_mode', 'rule']],
    ['switch_mode global', ['action', 'switch_mode', 'global']],
    ['switch_mode direct', ['action', 'switch_mode', 'direct']],
    ['switch_tun true', ['action', 'switch_tun', 'true']],
    ['switch_tun false', ['action', 'switch_tun', 'false']],
    ['log_size', ['service', 'logs_size']],
    ['clean_logs', ['service', 'clear_logs']],
  ];
  for (const [command, args] of cases) {
    const lines = [];
    const result = action.runActionScript(command, (line, stream) => lines.push({ line, stream }));
    const call = calls.at(-1);
    assert.deepEqual(nativeArgs(call), args);
    finish(call, ['running'], ['warning']);
    assert.equal((await result).stdout, 'running');
    assert.deepEqual(lines, [{ line: 'running', stream: 'stdout' }, { line: 'warning', stream: 'stderr' }]);
  }
});

test('native failures and empty profile results propagate to callers', async (t) => {
  const calls = captureSpawns(t);
  const patched = action.patchBoxConfig({ clash_api_port: '0' });
  finish(calls[0], [], ['invalid configuration'], 1);
  await assert.rejects(patched, /Patch box.config/);
  const imported = action.importMihomoConfigFile('a.yaml', 'mode: rule');
  finish(calls[1]);
  await assert.rejects(imported, /empty result/);
  const read = action.readLogFile('/invalid');
  finish(calls[2], [], ['path rejected'], 1);
  await assert.rejects(read, /Read log file failed/);
  const emptyConfig = action.readBoxConfig();
  finish(calls[3]);
  await assert.rejects(emptyConfig, /empty file content/);
});


const nextNativeCall = async (calls, count) => {
  for (let turn = 0; turn < 20 && calls.length < count; turn += 1) {
    await new Promise((resolve) => setImmediate(resolve));
  }
  assert.equal(calls.length, count, 'native request did not advance');
  return calls[count - 1];
};

test('large config writes upload bounded chunks sequentially and consume one JSON request', async (t) => {
  const calls = captureSpawns(t);
  const token = '0123456789abcdef0123456789abcdef';
  const content = `# 中文 '${'x'.repeat(180000)}'\r\nclash_api_port=9091\r\n`;
  const normalized = content.replace(/\r\n/g, '\n');
  const saved = action.writeBoxConfig(content);
  assert.deepEqual(nativeArgs(calls[0]), ['request', 'begin']);
  finish(calls[0], [token]);
  const chunks = [];
  let count = 2;
  for (;;) {
    const call = await nextNativeCall(calls, count);
    const args = nativeArgs(call);
    if (args[1] === 'run') {
      assert.deepEqual(args, ['request', 'run', token]);
      finish(call);
      break;
    }
    assert.deepEqual(args.slice(0, 3), ['request', 'append', token]);
    assert.equal(args.length, 4);
    assert.ok(args[3].length <= 48 * 1024);
    chunks.push(args[3]);
    // Only one chunk may be in flight at a time.
    await Promise.resolve();
    assert.equal(calls.length, count);
    finish(call);
    count += 1;
  }
  const request = JSON.parse(Buffer.from(chunks.join(''), 'base64').toString('utf8'));
  assert.deepEqual(request, ['config', 'write', encodeText(normalized)]);
  const aborted = await nextNativeCall(calls, count + 1);
  assert.deepEqual(nativeArgs(aborted), ['request', 'abort', token]);
  finish(aborted);
  await saved;
});

test('large request append failures abort the upload and preserve the original error', async (t) => {
  const calls = captureSpawns(t);
  const token = 'abcdef0123456789abcdef0123456789';
  const saved = action.patchBoxConfig({ clash_api_secret: `'${'x'.repeat(60000)}'` });
  finish(calls[0], [token]);
  const append = await nextNativeCall(calls, 2);
  assert.equal(nativeArgs(append)[1], 'append');
  finish(append, [], ['request too large'], 1);
  const abort = await nextNativeCall(calls, 3);
  assert.deepEqual(nativeArgs(abort), ['request', 'abort', token]);
  finish(abort, [], ['cleanup failed'], 2);
  await assert.rejects(saved, /request too large/);
  assert.equal(calls.length, 3);
});

test('chunked native commands stream only request run output and preserve its result', async (t) => {
  const calls = captureSpawns(t);
  const token = 'b'.repeat(32);
  const lines = [];
  const args = ['profiles', 'import', encodeText('stream.yaml'), encodeText('#' + 'x'.repeat(40000))];
  const pending = native.runNativeCommand(args, (line, stream) => lines.push({ line, stream }));
  finish(calls[0], [token]);
  let count = 2;
  for (;;) {
    const call = await nextNativeCall(calls, count);
    const request = nativeArgs(call);
    if (request[1] === 'run') {
      assert.deepEqual(request, ['request', 'run', token]);
      assert.deepEqual(lines, []);
      call.child.stdout.emit('data', 'first');
      call.child.stderr.emit('data', 'warning');
      call.child.stdout.emit('data', '');
      assert.deepEqual(lines, [
        { line: 'first', stream: 'stdout' }, { line: 'warning', stream: 'stderr' }, { line: '', stream: 'stdout' },
      ]);
      call.child.emit('exit', 7);
      break;
    }
    assert.equal(request[1], 'append');
    finish(call, ['append diagnostic']);
    count += 1;
  }
  const abort = await nextNativeCall(calls, count + 1);
  assert.deepEqual(nativeArgs(abort), ['request', 'abort', token]);
  finish(abort, ['abort diagnostic']);
  assert.deepEqual(await pending, { errno: 7, stdout: 'first\n', stderr: 'warning' });
  assert.equal(lines.length, 3);
});

test('failed request begin preserves output and status without append or abort', async (t) => {
  const calls = captureSpawns(t);
  const pending = native.runNativeCommand(['config', 'write', 'x'.repeat(60000)]);
  assert.deepEqual(nativeArgs(calls[0]), ['request', 'begin']);
  finish(calls[0], ['unavailable'], ['storage full'], 2);
  assert.deepEqual(await pending, { errno: 2, stdout: 'unavailable', stderr: 'storage full' });
  assert.equal(calls.length, 1);
});

test('large requests reject malformed begin tokens before they enter a shell command', async (t) => {
  const calls = captureSpawns(t);
  for (const token of ['invalid; id', 'A'.repeat(32), 'a'.repeat(31), `${'a'.repeat(32)}\nextra`]) {
    const saved = action.writeBoxConfig('#' + 'x'.repeat(60000));
    const call = calls.at(-1);
    assert.deepEqual(nativeArgs(call), ['request', 'begin']);
    finish(call, [token]);
    await assert.rejects(saved, /Invalid native request token/);
  }
  assert.equal(calls.length, 4);
});

test('request run bridge failures still abort without masking the bridge error', async (t) => {
  const calls = captureSpawns(t);
  const token = 'a'.repeat(32);
  const saved = action.writeBoxConfig('#' + 'x'.repeat(60000));
  finish(calls[0], [token]);
  let count = 2;
  for (;;) {
    const call = await nextNativeCall(calls, count);
    const args = nativeArgs(call);
    if (args[1] === 'run') {
      call.child.emit('error', new Error('request bridge unavailable'));
      break;
    }
    assert.equal(args[1], 'append');
    finish(call);
    count += 1;
  }
  const abort = await nextNativeCall(calls, count + 1);
  assert.deepEqual(nativeArgs(abort), ['request', 'abort', token]);
  finish(abort);
  await assert.rejects(saved, /request bridge unavailable/);
});
