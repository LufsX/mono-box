import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { compile } from 'svelte/compiler';
import { render } from 'svelte/server';

const { outputText } = ts.transpileModule(
  readFileSync(new URL('../src/lib/terminal.ts', import.meta.url), 'utf8'),
  { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } },
);
const { formatTerminalLine } = await import(
  `data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`
);

const terminalSource = readFileSync(
  new URL('../src/lib/components/home/LogTerminal.svelte', import.meta.url), 'utf8',
);
const { js } = compile(terminalSource, { generate: 'server', filename: 'LogTerminal.svelte' });
const { outputText: clipboardCode } = ts.transpileModule(
  readFileSync(new URL("../src/lib/clipboard.ts", import.meta.url), "utf8"),
  { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } },
);
const clipboardUrl = `data:text/javascript;base64,${Buffer.from(clipboardCode).toString("base64")}`;
const serverCode = js.code.replace(/from (['"])(svelte(?:\/[^'"]*)?)\1/g,
  (_, quote, specifier) => `from ${quote}${import.meta.resolve(specifier)}${quote}`)
  .replace(/from (['"])\$lib\/clipboard\1/g, () => `from ${JSON.stringify(clipboardUrl)}`);
const { default: LogTerminal } = await import(
  `data:text/javascript;base64,${Buffer.from(serverCode).toString('base64')}`
);

test('plain text, Unicode, whitespace and empty messages are preserved', () => {
  const msg = '中文 🚀  a\tb\nc';
  assert.deepEqual(formatTerminalLine(msg), { msg, type: 'info', segments: [{ text: msg }] });
  assert.deepEqual(formatTerminalLine(''), { msg: '', type: 'info', segments: [] });
});

test('all basic and bright foreground colors are decoded', () => {
  const colors = Array.from({ length: 16 }, (_, index) => `var(--terminal-ansi-${index})`);
  colors.forEach((color, index) => {
    const code = index < 8 ? 30 + index : 90 + index - 8;
    assert.deepEqual(formatTerminalLine(`\x1b[${code}mtext`).segments, [{ text: 'text', color }]);
  });
});

test('compound SGR applies bold/color and supports full, color-only and bold-only reset', () => {
  const result = formatTerminalLine('a\x1b[1;31mred\x1b[22mnormal red\x1b[39mplain\x1b[1;94mblue\x1b[0mreset\x1b[1mbold\x1b[mend');
  assert.equal(result.msg, 'arednormal redplainblueresetboldend');
  assert.deepEqual(result.segments, [
    { text: 'a' },
    { text: 'red', color: 'var(--terminal-ansi-1)', bold: true },
    { text: 'normal red', color: 'var(--terminal-ansi-1)' },
    { text: 'plain' },
    { text: 'blue', color: 'var(--terminal-ansi-12)', bold: true },
    { text: 'reset' },
    { text: 'bold', bold: true },
    { text: 'end' },
  ]);
});

test('256 colors cover palette, cube and grayscale boundaries', () => {
  for (const [index, color] of [[0, 'var(--terminal-ansi-0)'], [9, 'var(--terminal-ansi-9)'], [16, '#000000'],
    [67, '#5f87af'], [196, '#ff0000'], [231, '#ffffff'], [232, '#080808'], [255, '#eeeeee']]) {
    assert.deepEqual(formatTerminalLine(`\x1b[38;5;${index}mx`).segments, [{ text: 'x', color }]);
  }
});

test('truecolor supports compound semicolon and colon RGB syntax', () => {
  for (const sgr of ['1;38;2;12;34;255', '1;38:2:12:34:255', '1;38:2::12:34:255']) {
    assert.deepEqual(formatTerminalLine(`\x1b[${sgr}mx`).segments,
      [{ text: 'x', color: '#0c22ff', bold: true }]);
  }
});

test('unsupported background/underline RGB parameters do not change foreground or bold', () => {
  assert.deepEqual(formatTerminalLine('\x1b[32;48;2;1;31;0;58;5;1mx').segments,
    [{ text: 'x', color: 'var(--terminal-ansi-2)' }]);
  for (const codes of ['38;5;256', '38;2;999;31;1', '38;2;12;34', '38;5']) {
    assert.deepEqual(formatTerminalLine(`\x1b[${codes}mx`).segments, [{ text: 'x' }]);
  }
});

test('redundant SGR and stripped CSI do not fragment identical spans', () => {
  assert.deepEqual(formatTerminalLine('\x1b[31ma\x1b[31mb\x1b[2Kc').segments,
    [{ text: 'abc', color: 'var(--terminal-ansi-1)' }]);
});

test('OSC titles, hyperlinks and clipboard contents are removed for BEL/ST/C1 terminators', () => {
  const msg = 'a\x1b]0;title\x07b\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\' +
    '\x9d52;c;secret\x9cc';
  assert.equal(formatTerminalLine(msg).msg, 'ablinkc');
  assert.equal(formatTerminalLine('visible\x1b]0;unterminated secret').msg, 'visible');
});

test('non-SGR CSI, other escape strings and control bytes leave no garbage', () => {
  const msg = '\x1b[2J\x1b[Hone\x1b[?25l\x9b2K\x1b[1;4Htwo\x1b(B\x1b7' +
    '\x1bPprivate payload\x1b\\\x1b_hidden\x1b\\\r\x00\x07\x08\x7f\x85\t\n';
  assert.equal(formatTerminalLine(msg).msg, 'onetwo\t\n');
  assert.equal(formatTerminalLine('text\x1b[31;').msg, 'text');
});

test('explicit levels override warning/error fallback, including structured and timestamped logs', () => {
  const cases = [
    ['[Info] listening', 'info'],
    ['INFO[0000] started', 'info'],
    ['Warn: slow connection', 'warning'],
    ['[WARNING] retrying', 'warning'],
    ['Error: connection failed', 'error'],
    ['[ERROR] service started with an error', 'error'],
    ['FATAL cannot open config', 'error'],
    ['panic: broken invariant', 'error'],
    ['DEBUG previous error was handled', 'info'],
    ['[TRACE] lookup', 'info'],
    ['NOTICE ready', 'info'],
    ['time="2026-06-01T00:00:00Z" level=info msg="error is just text"', 'info'],
    ['{"level":"error","msg":"failed"}', 'error'],
    ['severity=warning message=retry', 'warning'],
    ['2026-06-01T12:34:56.000+08:00 ERROR failed', 'error'],
    ['[2026-06-01 12:34:56] Debug: previous failure', 'info'],
    ['12:34:56.123 WARN slow', 'warning'],
    ['INFO previous message was [Error]', 'info'],
    ['\x1b[31m[INFO]\x1b[0m ready', 'info'],
  ];
  for (const [msg, expected] of cases) {
    assert.equal(formatTerminalLine(msg, 'warning').type, expected, msg);
    assert.equal(formatTerminalLine(msg, 'error').type, expected, msg);
  }
});

test('unmarked messages use caller fallback; mentioning error does not classify a line as error', () => {
  for (const msg of ['listening on port 9090', 'previous error count: 0', 'download error-handler.js']) {
    assert.equal(formatTerminalLine(msg).type, 'info');
    assert.equal(formatTerminalLine(msg, 'warning').type, 'warning');
    assert.equal(formatTerminalLine(msg, 'error').type, 'error');
  }
});

test('unmarked started/stopped lifecycle output is success, while explicit errors remain errors', () => {
  for (const msg of ['Mihomo started', '[box] service stopped', 'Started successfully']) {
    assert.equal(formatTerminalLine(msg, 'warning').type, 'success', msg);
  }
  assert.equal(formatTerminalLine('service not started', 'warning').type, 'warning');
  assert.equal(formatTerminalLine('Error: service stopped unexpectedly').type, 'error');
  assert.equal(formatTerminalLine('service failed before it started', 'error').type, 'error');
});

test('command and success fallback cannot be changed by level-looking message text', () => {
  for (const fallback of ['cmd', 'success']) {
    for (const msg of ['ERROR configuration', '[Warn] retry', 'level=error', 'service stopped']) {
      assert.equal(formatTerminalLine(msg, fallback).type, fallback);
    }
  }
});

test('parsing has no state between calls and does not reuse mutable segments', () => {
  const colored = formatTerminalLine('\x1b[1;31mcolored');
  colored.segments[0].text = 'changed';
  assert.deepEqual(formatTerminalLine('plain').segments, [{ text: 'plain' }]);
  assert.deepEqual(formatTerminalLine('\x1b[1;31mcolored').segments,
    [{ text: 'colored', color: 'var(--terminal-ansi-1)', bold: true }]);
});

test('malicious HTML stays literal text and Svelte escapes it inside spans', () => {
  const payload = '<img src=x onerror=alert(1)><script>alert("x")</script>&';
  const formatted = formatTerminalLine(`\x1b[1;38;2;12;34;56m${payload}\x1b[0m`);
  assert.equal(formatted.msg, payload);
  assert.equal(formatted.segments[0].text, payload);
  assert.equal(formatted.segments[0].color, '#0c2238');
  const { body } = render(LogTerminal, { props: { logs: [{ time: '12:00:00', ...formatted }] } });
  assert.ok(body.includes('&lt;img'));
  assert.ok(body.includes('&lt;script>'));
  assert.ok(!body.includes('<img'));
  assert.ok(!body.includes('<script'));
  assert.match(body, /color: #0c2238/);
  assert.match(body, /font-weight: 700/);
  assert.ok(!terminalSource.includes('{@html'));
});

test('terminal renders English labels, empty state, legacy entries and semantic color classes', () => {
  const empty = render(LogTerminal, { props: { logs: [] } }).body;
  for (const label of ['Terminal Logs', 'CLEAR', 'Waiting for output...']) assert.ok(empty.includes(label));
  for (const [type, color] of [['cmd', 'cyan'], ['success', 'emerald'], ['warning', 'amber'], ['error', 'rose']]) {
    const { body } = render(LogTerminal, { props: { logs: [{ time: '12:00:00', msg: '<plain>', type }] } });
    assert.ok(body.includes('&lt;plain>'));
    assert.ok(body.includes(`text-${color}-700`));
    assert.ok(body.includes(`dark:text-${color}-`));
  }
});
