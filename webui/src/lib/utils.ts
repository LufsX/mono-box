export function formatBytes(value: number): string {
  if (!value || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let size = value;
  let idx = 0;
  while (size >= 1024 && idx < units.length - 1) {
    size /= 1024;
    idx += 1;
  }
  return `${size.toFixed(idx <= 1 ? 0 : 1)} ${units[idx]}`;
}

// Used by Rules rule/provider tags.
export function clashPillTagClass(kind: "type" | "meta" | "status", value: string, rounded: boolean): string {
  const base = `text-[9px] px-1.5 py-0.5 border uppercase font-semibold ${rounded ? "rounded-md" : ""}`;

  const style = {
    status: "border-red-300 dark:border-red-900/40 bg-red-50 dark:bg-red-950/30 text-red-700 dark:text-red-300",
    green: "border-emerald-200 dark:border-emerald-900/40 bg-emerald-50/60 dark:bg-emerald-950/20 text-emerald-700 dark:text-emerald-300",
    blue: "border-blue-200 dark:border-blue-900/40 bg-blue-50/60 dark:bg-blue-950/20 text-blue-700 dark:text-blue-300",
    amber: "border-amber-200 dark:border-amber-900/40 bg-amber-50/60 dark:bg-amber-950/20 text-amber-800 dark:text-amber-300",
    purple: "border-purple-200 dark:border-purple-900/40 bg-purple-50/60 dark:bg-purple-950/20 text-purple-800 dark:text-purple-300",
    gray: "border-slate-300 dark:border-zinc-700 bg-slate-100 dark:bg-zinc-800 text-slate-600 dark:text-zinc-300",
  } as const;

  if (kind === "status") {
    return `${base} ${style.status}`;
  }

  if (kind !== "type") {
    return `${base} ${style.gray}`;
  }

  const type = String(value || "")
    .toUpperCase()
    .trim();

  const startsWithAny = (prefixes: string[]) => prefixes.some((prefix) => type.startsWith(prefix));
  const includesAny = (needles: string[]) => needles.some((needle) => type.includes(needle));

  const greenExact = new Set(["GEOSITE", "HTTP"]);
  const blueExact = new Set(["GEOIP", "FILE"]);
  const amberExact = new Set(["UID", "NETWORK", "DSCP"]);
  const purpleExact = new Set(["AND", "OR", "NOT", "SUB-RULE", "MATCH", "INLINE", "CLASSICAL"]);

  // green
  if (type.startsWith("DOMAIN") || greenExact.has(type)) {
    return `${base} ${style.green}`;
  }

  // blue
  const isDirectional = startsWithAny(["SRC-", "DST-"]);
  if (blueExact.has(type) || type.startsWith("IP") || (isDirectional && includesAny(["IP", "ASN", "GEOIP"]))) {
    return `${base} ${style.blue}`;
  }

  // amber
  if (type.endsWith("PORT") || startsWithAny(["IN-", "PROCESS"]) || amberExact.has(type)) {
    return `${base} ${style.amber}`;
  }

  // purple
  if (purpleExact.has(type)) {
    return `${base} ${style.purple}`;
  }

  // ruleset
  if (type === "RULESET") {
    return `${base} ${style.amber}`;
  }

  return `${base} ${style.gray}`;
}

export function clashConnectionTagClass(kind: "network" | "type" | "rule" | "time" | "chain", value: string, rounded: boolean, size: "compact" | "normal" = "compact"): string {
  const lower = String(value || "").toLowerCase();
  const sizing = size === "normal" ? "px-2 py-1 text-xs" : "px-1.5 py-0.5 text-[10px]";
  const base = `inline-flex items-center min-w-0 border leading-none ${sizing} ${rounded ? "rounded-md" : ""} font-semibold`;

  if (kind === "time") {
    return `${base} font-mono uppercase whitespace-nowrap border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950/30 text-slate-500 dark:text-zinc-400`;
  }

  if (kind === "chain") {
    return `${base} font-mono border-slate-300 dark:border-zinc-700 bg-white dark:bg-zinc-950/30 text-slate-700 dark:text-zinc-200`;
  }

  if (kind === "rule") {
    return `${base} font-bold truncate border-blue-200 dark:border-blue-900/40 bg-blue-50 dark:bg-blue-950/30 text-blue-700 dark:text-blue-300`;
  }

  if (kind === "network") {
    if (lower === "udp") return `${base} uppercase border-amber-200 dark:border-amber-900/40 bg-amber-50 dark:bg-amber-950/30 text-amber-700 dark:text-amber-300`;
    if (lower === "tcp") return `${base} uppercase border-blue-200 dark:border-blue-900/40 bg-blue-50 dark:bg-blue-950/30 text-blue-700 dark:text-blue-300`;
    return `${base} uppercase border-slate-300 dark:border-zinc-700 bg-slate-100 dark:bg-zinc-800 text-slate-600 dark:text-zinc-300`;
  }

  if (lower.includes("socks")) return `${base} border-emerald-200 dark:border-emerald-900/40 bg-emerald-50 dark:bg-emerald-950/30 text-emerald-700 dark:text-emerald-300`;
  if (lower.includes("http")) return `${base} border-blue-200 dark:border-blue-900/40 bg-blue-50 dark:bg-blue-950/30 text-blue-700 dark:text-blue-300`;
  if (lower.includes("tun")) return `${base} border-purple-200 dark:border-purple-900/40 bg-purple-50 dark:bg-purple-950/30 text-purple-700 dark:text-purple-300`;
  return `${base} border-slate-300 dark:border-zinc-700 bg-slate-100 dark:bg-zinc-800 text-slate-600 dark:text-zinc-300`;
}
