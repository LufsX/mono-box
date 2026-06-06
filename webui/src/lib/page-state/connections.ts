import type { ClashConnection } from "$lib/api";

export type EnrichedConnection = ClashConnection & {
  upSpeed: number;
  downSpeed: number;
};

export type SortMode = "default" | "host-asc" | "host-desc";

export function sortConnections(connections: EnrichedConnection[], sortMode: SortMode, defaultOrderIndex: Map<string, number>): void {
  if (connections.length <= 1) return;
  if (sortMode === "default") {
    connections.sort((a, b) => (defaultOrderIndex.get(a.id) ?? 0) - (defaultOrderIndex.get(b.id) ?? 0));
    return;
  }

  const dir = sortMode === "host-asc" ? 1 : -1;
  connections.sort((a, b) => {
    const aHost = (a.metadata.host || a.metadata.destinationIP || "").toLowerCase();
    const bHost = (b.metadata.host || b.metadata.destinationIP || "").toLowerCase();
    const byHost = aHost.localeCompare(bHost, "en", { sensitivity: "base" });
    if (byHost !== 0) return byHost * dir;
    return (defaultOrderIndex.get(a.id) ?? 0) - (defaultOrderIndex.get(b.id) ?? 0);
  });
}

export function formatConnectionDuration(startTime: string): string {
  const start = new Date(startTime).getTime();
  const ageSeconds = Math.floor((Date.now() - start) / 1000);
  if (ageSeconds < 60) return `${ageSeconds}s`;
  const m = Math.floor(ageSeconds / 60);
  const s = ageSeconds % 60;
  if (m < 60) return `${m}m ${s}s`;
  const h = Math.floor(m / 60);
  const m2 = m % 60;
  return `${h}h ${m2}m`;
}

function escapeHtml(input: string): string {
  return input.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

export function highlightJson(value: unknown): string {
  const raw = JSON.stringify(value, null, 2) || "";
  const escaped = escapeHtml(raw);
  return escaped.replace(/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"\s*:?)|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?/g, (match) => {
    const isKey = match.endsWith(":");
    if (isKey) return `<span class="text-slate-800 dark:text-zinc-100 font-semibold">${match}</span>`;
    if (match === "true" || match === "false") return `<span class="text-emerald-600 dark:text-emerald-400">${match}</span>`;
    if (match === "null") return `<span class="text-rose-600 dark:text-rose-400">${match}</span>`;
    if (match.startsWith('"')) return `<span class="text-blue-700 dark:text-blue-300">${match}</span>`;
    return `<span class="text-amber-700 dark:text-amber-300">${match}</span>`;
  });
}
