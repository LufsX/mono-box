export function getErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

export function formatExecResult(action: string, result: { errno: number; stdout?: string; stderr?: string }): string {
  const details = [result.stderr, result.stdout]
    .filter((part) => typeof part === "string" && part.trim().length > 0)
    .join("\n")
    .trim();

  return details ? `${action} failed (errno=${result.errno}): ${details}` : `${action} failed (errno=${result.errno})`;
}

export function buildExecError(action: string, result: { errno: number; stdout?: string; stderr?: string }): Error {
  return new Error(formatExecResult(action, result));
}

export function classifyConnectionError(error: unknown): { ok: false; reason: "unauthorized" | "unreachable"; message: string } {
  const msg = getErrorMessage(error);
  if (msg.toLowerCase().includes("unauthorized") || msg.includes("401")) {
    return { ok: false, reason: "unauthorized", message: "Clash API 认证失败，请检查 Secret" };
  }
  return { ok: false, reason: "unreachable", message: `无法连接 Clash API: ${msg}` };
}
