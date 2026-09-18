import type { ClashLogEntry } from "$lib/api";

export const LOG_ROW_LIMIT = 600;
export const LOG_PAYLOAD_LIMIT = 8192;
export type LogRow = ClashLogEntry & { id: number; time: string };

/** Bound both retained row count and individual messages, including malformed input. */
export function normalizeLogEntry(data: unknown): ClashLogEntry {
  const value = data && typeof data === "object" ? data as Record<string, unknown> : {};
  let payload: string;
  try { payload = typeof value.payload === "string" ? value.payload : JSON.stringify(data) ?? String(data); }
  catch { payload = String(data); }
  return {
    type: typeof value.type === "string" ? value.type.slice(0, 32) : "info",
    payload: payload.length > LOG_PAYLOAD_LIMIT ? `${payload.slice(0, LOG_PAYLOAD_LIMIT)}\n[日志过长，已截断]` : payload,
  };
}

export function mergeLogRows(rows: LogRow[], pending: LogRow[]): LogRow[] {
  return [...pending].reverse().concat(rows).slice(0, LOG_ROW_LIMIT);
}
