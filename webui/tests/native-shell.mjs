import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';

export const STANDARD_BINARY = '/data/adb/modules/mono_box/bin/monobox';
export const LITE_BINARY = '/data/adb/lite_modules/mono_box/bin/monobox';

export function runNativeShell(command, {
  standard = true, lite = true, version = '28.1', magisk = true, magiskExit = 0,
  binaryExit = 0, binaryStderr = '',
} = {}) {
  const directory = mkdtempSync(join(tmpdir(), 'mono-box-native-'));
  const paths = {
    standard: join(directory, 'modules/mono_box/bin/monobox'),
    lite: join(directory, 'lite_modules/mono_box/bin/monobox'),
    pending: join(directory, 'modules_update/mono_box/bin/monobox'),
    legacy: join(directory, 'box/bin/monobox'),
  };
  const install = (path, content, executable = true) => {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, content, { mode: executable ? 0o755 : 0o644 });
  };
  const binary = '#!/bin/sh\nprintf "%s\\0" "$0" "$@"\nprintf "%s" "$MONO_TEST_STDERR" >&2\nexit "$MONO_TEST_EXIT"\n';
  try {
    if (standard) install(paths.standard, binary, standard !== 'non-executable');
    if (lite) install(paths.lite, binary, lite !== 'non-executable');
    // These are always executable: neither may be selected even if active binaries are missing.
    install(paths.pending, binary);
    install(paths.legacy, binary);
    if (magisk) install(join(directory, 'magisk'),
      '#!/bin/sh\n[ "$1" = "-v" ] || exit 99\nprintf "%s\\n" "$MONO_TEST_MAGISK_VERSION"\nprintf "%s\\n" "version diagnostic" >&2\nexit "$MONO_TEST_MAGISK_EXIT"\n');
    install(join(directory, 'su'), '#!/bin/sh\nprintf "%s" "unexpected nested su" >&2\nexit 95\n');
    const rewritten = command
      .replaceAll(STANDARD_BINARY, paths.standard)
      .replaceAll(LITE_BINARY, paths.lite)
      .replaceAll('/data/adb/modules_update/mono_box/bin/monobox', paths.pending)
      .replaceAll('/data/adb/box/bin/monobox', paths.legacy);
    const result = spawnSync('/bin/sh', ['-c', rewritten + ' '], {
      encoding: 'utf8', timeout: 5000,
      env: {
        PATH: directory,
        MONO_TEST_MAGISK_VERSION: version, MONO_TEST_MAGISK_EXIT: String(magiskExit),
        MONO_TEST_EXIT: String(binaryExit), MONO_TEST_STDERR: binaryStderr,
      },
    });
    assert.ifError(result.error);
    const argv = result.stdout ? result.stdout.split('\0') : [];
    if (argv.length) assert.equal(argv.pop(), '');
    return { ...result, selected: argv.shift(), args: argv, paths };
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}
