export type LogType = "info" | "success" | "warning" | "error" | "cmd";

export interface TerminalSegment {
  text: string;
  color?: string;
  bold?: boolean;
}

export interface LogEntry {
  time: string;
  msg: string;
  type: LogType;
  segments?: TerminalSegment[];
}

// The first 16 colors adapt to the terminal theme; extended colors retain their RGB values.
const ANSI_COLORS = Array.from({ length: 16 }, (_, index) => `var(--terminal-ansi-${index})`);

function indexedColor(index: number): string | undefined {
  if (!Number.isInteger(index) || index < 0 || index > 255) return;
  if (index < 16) return ANSI_COLORS[index];
  if (index >= 232) {
    const value = (8 + (index - 232) * 10).toString(16).padStart(2, "0");
    return `#${value.repeat(3)}`;
  }
  const cube = index - 16;
  const levels = [0, 95, 135, 175, 215, 255];
  return "#" + [
    levels[Math.floor(cube / 36)],
    levels[Math.floor(cube / 6) % 6],
    levels[cube % 6],
  ].map((value) => value.toString(16).padStart(2, "0")).join("");
}

function classify(message: string, fallback: LogType): LogType {
  if (fallback === "cmd" || fallback === "success") return fallback;

  const levels = "trace|debug|info|notice|warn(?:ing)?|err(?:or)?|fatal|panic|success";
  // Match structured fields, bracketed levels, or a level at the start after a timestamp.
  const field = message.match(new RegExp(`(?:^|[\\s,{])["']?(?:level|severity)["']?\\s*[:=]\\s*["']?(${levels})\\b`, "i"));
  const bracket = message.match(new RegExp(`\\[\\s*(${levels})\\s*\\]`, "i"));
  const prefix = message
    .replace(/^\s*(?:\[\d[^\]]*\]|\d{4}-\d{2}-\d{2}[T ][\d:.]+(?:Z|[+-]\d{2}:?\d{2})?|\d{2}:\d{2}:\d{2}(?:\.\d+)?)\s*/, "")
    .match(new RegExp(`^\\s*(${levels})(?=\\s|:|\\[|$)`, "i"));
  const level = (field?.[1] ?? prefix?.[1] ?? bracket?.[1])?.toLowerCase();
  if (level === "error" || level === "err" || level === "fatal" || level === "panic") return "error";
  if (level === "warn" || level === "warning") return "warning";
  if (level === "success") return "success";
  if (level) return "info";

  // Lifecycle output has no formal level in some action scripts.
  if (/\b(?:started|stopped)\b/i.test(message) &&
      !/\b(?:not|never|failed|error|cannot|unable)\b/i.test(message)) return "success";
  return fallback;
}

/** Decode one line into plain text and safe text spans. No state carries between calls. */
export function formatTerminalLine(
  message: string,
  fallback: LogType = "info",
): { msg: string; type: LogType; segments: TerminalSegment[] } {
  const segments: TerminalSegment[] = [];
  let color: string | undefined;
  let bold = false;
  let text = "";

  function flush() {
    if (!text) return;
    const previous = segments.at(-1);
    if (previous && previous.color === color && Boolean(previous.bold) === bold) {
      previous.text += text;
    } else {
      segments.push({ text, ...(color ? { color } : {}), ...(bold ? { bold: true } : {}) });
    }
    text = "";
  }

  function sgr(parameters: string) {
    if (!/^[\d;:]*$/.test(parameters)) return;
    // Accept both semicolon SGR and colon RGB syntax (including the empty color-space slot).
    const values = parameters.replace(/(38|48|58):2::/g, "$1:2:").split(/[;:]/).map(Number);
    for (let i = 0; i < values.length; i++) {
      const code = values[i];
      if (code === 0) {
        color = undefined;
        bold = false;
      } else if (code === 1) {
        bold = true;
      } else if (code === 22) {
        bold = false;
      } else if (code === 39) {
        color = undefined;
      } else if (code >= 30 && code <= 37) {
        color = ANSI_COLORS[code - 30];
      } else if (code >= 90 && code <= 97) {
        color = ANSI_COLORS[code - 90 + 8];
      } else if (code === 38 || code === 48 || code === 58) {
        // Consume unsupported background/underline colors too, so RGB values aren't SGR codes.
        const mode = values[++i];
        let nextColor: string | undefined;
        if (mode === 5) {
          nextColor = indexedColor(values[++i]);
        } else if (mode === 2) {
          const rgb = values.slice(i + 1, i + 4);
          i += 3;
          if (rgb.length === 3 && rgb.every((value) => Number.isInteger(value) && value >= 0 && value <= 255)) {
            nextColor = "#" + rgb.map((value) => value.toString(16).padStart(2, "0")).join("");
          }
        }
        if (code === 38 && nextColor) color = nextColor;
      }
    }
  }

  for (let i = 0; i < message.length;) {
    const code = message.charCodeAt(i);
    const escape = code === 0x1b;
    const next = message[i + 1];
    const csi = code === 0x9b || (escape && next === "[");
    const controlString = code === 0x9d || code === 0x90 || code === 0x98 || code === 0x9e || code === 0x9f ||
      (escape && (next === "]" || next === "P" || next === "X" || next === "^" || next === "_"));

    if (controlString) {
      const osc = code === 0x9d || next === "]";
      i += escape ? 2 : 1;
      // OSC may end in BEL; all control strings may end in ST.
      while (i < message.length) {
        if (message.charCodeAt(i) === 0x9c || (osc && message.charCodeAt(i) === 7)) {
          i++;
          break;
        }
        if (message[i] === "\x1b" && message[i + 1] === "\\") {
          i += 2;
          break;
        }
        i++;
      }
    } else if (csi) {
      const start = i + (escape ? 2 : 1);
      i = start;
      while (i < message.length && /[\x20-\x3f]/.test(message[i])) i++;
      if (i < message.length && /[\x40-\x7e]/.test(message[i])) {
        if (message[i] === "m") {
          flush();
          sgr(message.slice(start, i));
        }
        i++;
      }
    } else if (escape) {
      i++;
      while (i < message.length && /[\x20-\x2f]/.test(message[i])) i++;
      if (i < message.length && /[\x30-\x7e]/.test(message[i])) i++;
    } else {
      // Preserve tabs/newlines, but remove C0/C1 controls, including CR and BEL.
      if (code === 9 || code === 10 || (code >= 0x20 && (code < 0x7f || code > 0x9f))) text += message[i];
      i++;
    }
  }
  flush();
  const msg = segments.map((segment) => segment.text).join("");
  return { msg, type: classify(msg, fallback), segments };
}
