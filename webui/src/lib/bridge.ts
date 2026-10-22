import { spawn } from "kernelsu";
import type { CommandResult } from "./api/clash.types";

export type CommandOutputListener = (line: string, stream: "stdout" | "stderr") => void;

export interface CommandOptions {
  cwd?: string;
  env?: Record<string, string>;
  onLine?: CommandOutputListener;
}

const hookedBridges = new WeakSet<object>();
let activeSpawnErrorHandler: ((error: unknown) => void) | undefined;

function hookSynchronousSpawnErrors(nativeBridge: { spawn?: unknown }) {
  if (hookedBridges.has(nativeBridge)) return;
  const originalSpawn = nativeBridge.spawn;
  if (typeof originalSpawn !== "function") return;

  const bridgeWindow = window as unknown as Record<string, unknown>;
  nativeBridge.spawn = function (...args: any[]) {
    const callbackName = args[3];
    if (typeof callbackName === "string") {
      const child = bridgeWindow[callbackName] as ReturnType<typeof spawn> | undefined;
      if (activeSpawnErrorHandler) child?.on("error", activeSpawnErrorHandler);
    }
    return originalSpawn.apply(nativeBridge, args);
  };
  hookedBridges.add(nativeBridge);
}

export function runCommand(command: string, { onLine, ...options }: CommandOptions = {}): Promise<CommandResult> {
  return new Promise((resolve, reject) => {
    const nativeBridge = typeof window === "undefined" ? undefined : (window as Window & { ksu?: { spawn?: unknown } }).ksu;
    if (typeof nativeBridge?.spawn !== "function") {
      reject(new Error("KernelSU spawn bridge is unavailable"));
      return;
    }

    const stdout: string[] = [];
    const stderr: string[] = [];
    // Native spawn appends a space even with no args; keep it off heredoc terminators.
    let child: ReturnType<typeof spawn>;

    hookSynchronousSpawnErrors(nativeBridge);
    const previousErrorHandler = activeSpawnErrorHandler;
    activeSpawnErrorHandler = reject;
    try {
      child = spawn(`${command}\n`, [], options);
    } catch (error) {
      reject(error);
      return;
    } finally {
      activeSpawnErrorHandler = previousErrorHandler;
    }

    // KernelSU emits lines without delimiters, just like exec's output arrays.
    child.stdout.on("data", (line) => {
      stdout.push(line);
      onLine?.(line, "stdout");
    });
    child.stderr.on("data", (line) => {
      stderr.push(line);
      onLine?.(line, "stderr");
    });
    child.on("exit", (errno) => {
      resolve({ errno, stdout: stdout.join("\n"), stderr: stderr.join("\n") });
    });
    child.on("error", reject);
  });
}
