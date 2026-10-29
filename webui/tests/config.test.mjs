import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

async function load(relative, imports = {}) {
  const source = readFileSync(new URL(relative, import.meta.url), 'utf8');
  let { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
  for (const [specifier, url] of Object.entries(imports)) {
    outputText = outputText.replaceAll(JSON.stringify(specifier), JSON.stringify(url));
  }
  const url = `data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`;
  return { url, module: await import(url) };
}
const parser = await load('../src/lib/api/config-parser.ts');
const form = await load('../src/lib/api/box-config-form.ts');
const errors = await load('../src/lib/api/error-utils.ts');
const accessor = await load('../src/lib/api/box-config.ts', {
  './config-parser': parser.url, './error-utils': errors.url,
});

test('form values round trip literal quotes, backslashes, dollars and comments', () => {
  for (const value of ['中文 "引号" # comment', "single'quote", 'a\\b$c`d', 'spaces and ; mappings']) {
    const encoded = form.module.quoteBoxConfigValue(value);
    assert.equal(parser.module.readConfigValue(`clash_api_secret=${encoded} # retained`, 'clash_api_secret'), value);
  }
  assert.equal(parser.module.stripConfigValue("'abc'\\''def'"), "abc'def");
});

test('native config patches bypass stale full-file state', async () => {
  const calls = [];
  const config = accessor.module.createBoxConfigAccessor({
    readBoxConfig: async () => 'clash_api_port=9090\n',
    writeBoxConfig: async () => { throw new Error('unexpected full write'); },
    updateBoxConfigValues: async (updates) => calls.push(updates),
  });
  await config.getBoxConfig();
  await config.updateBoxConfigValues({ target_wifi: '"external-safe"' });
  assert.deepEqual(calls, [{ target_wifi: '"external-safe"' }]);
});
