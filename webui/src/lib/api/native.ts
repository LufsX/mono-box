import { runCommand, type CommandOutputListener } from "../bridge";
import type { CommandResult } from "./clash.types";

const BOX_ROOT = "/data/adb/box";
const STANDARD_BINARY = "/data/adb/modules/mono_box/bin/monobox";
const LITE_BINARY = "/data/adb/lite_modules/mono_box/bin/monobox";

export function toBase64Utf8(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
}

export function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}

export function buildNativeCommand(args: string[]): string {
  // Resolve only active module binaries. The bridge already runs as root.
  return [
    `standard=${shellQuote(STANDARD_BINARY)}`,
    `lite=${shellQuote(LITE_BINARY)}`,
    'if [ -x "$standard" ] && [ -x "$lite" ]; then',
    '  case "$(PATH="/data/adb/magisk:$PATH" magisk -v 2>/dev/null)" in',
    '    *[Ll][Ii][Tt][Ee]*) native="$lite" ;;',
    '    *) native="$standard" ;;',
    '  esac',
    'elif [ -x "$standard" ]; then',
    '  native="$standard"',
    'elif [ -x "$lite" ]; then',
    '  native="$lite"',
    'else',
    '  printf "%s\\n" "Mono Box native binary not found in active module directories" >&2',
    '  exit 127',
    'fi',
    `exec "$native"${args.length ? ` ${args.map(shellQuote).join(" ")}` : ""}`,
  ].join("\n");
}

const NATIVE_REQUEST_CHUNK_SIZE = 48 * 1024;

function executeNativeCommand(args: string[], onLine?: CommandOutputListener): Promise<CommandResult> {
  return runCommand(buildNativeCommand(args), { cwd: BOX_ROOT, onLine });
}

export async function runNativeCommand(args: string[], onLine?: CommandOutputListener): Promise<CommandResult> {
  const encodedRequest = toBase64Utf8(JSON.stringify(args));
  if (encodedRequest.length <= NATIVE_REQUEST_CHUNK_SIZE) return executeNativeCommand(args, onLine);

  const started = await executeNativeCommand(["request", "begin"]);
  if (started.errno !== 0) return started;
  const token = started.stdout.trim();
  if (!/^[a-f0-9]{32}$/.test(token)) throw new Error("Invalid native request token");

  try {
    for (let offset = 0; offset < encodedRequest.length; offset += NATIVE_REQUEST_CHUNK_SIZE) {
      const appended = await executeNativeCommand([
        "request", "append", token, encodedRequest.slice(offset, offset + NATIVE_REQUEST_CHUNK_SIZE),
      ]);
      if (appended.errno !== 0) return appended;
    }
    return await executeNativeCommand(["request", "run", token], onLine);
  } finally {
    try {
      await executeNativeCommand(["request", "abort", token]);
    } catch {
      // Preserve the operation's result if the bridge also fails during cleanup.
    }
  }
}
