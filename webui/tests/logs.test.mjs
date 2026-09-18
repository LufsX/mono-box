import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const { outputText } = ts.transpileModule(readFileSync(new URL('../src/lib/page-state/logs.ts', import.meta.url), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
const { normalizeLogEntry, mergeLogRows, LOG_ROW_LIMIT, LOG_PAYLOAD_LIMIT } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('burst logs retain newest 600 rows in newest-first order without mutating buffers', () => {
  const pending = Array.from({ length: 1000 }, (_, id) => ({ id, type: 'info', payload: `${id}`, time: '00:00:00' }));
  const result = mergeLogRows([{ id: -1 }], pending);
  assert.equal(result.length, LOG_ROW_LIMIT);
  assert.equal(result[0].id, 999);
  assert.equal(result.at(-1).id, 400);
  assert.equal(pending[0].id, 0);
});

test('invalid entries, circular objects and oversized messages are bounded', () => {
  assert.equal(normalizeLogEntry(null).payload, 'null');
  assert.equal(normalizeLogEntry({ type: 1, payload: 2 }).type, 'info');
  const circular = {}; circular.self = circular;
  assert.doesNotThrow(() => normalizeLogEntry(circular));
  const result = normalizeLogEntry({ type: 'x'.repeat(1000), payload: 'a'.repeat(1000000) });
  assert.equal(result.type.length, 32);
  assert.ok(result.payload.length < LOG_PAYLOAD_LIMIT + 50);
});
