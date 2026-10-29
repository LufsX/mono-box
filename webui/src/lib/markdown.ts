import { lexer, type Token } from "marked";
import { decodeHTMLAttribute } from "entities";

export function parseMarkdown(source: string): Token[] {
  try {
    return lexer(source, { gfm: true });
  } catch {
    return [{ type: "text", raw: source, text: source, escaped: true }];
  }
}

// Decode character references before checking the protocol. Relative links need
// an HTTP source URL; they must never resolve against the manager's WebView URL.
export function markdownLinkUrl(href: string, baseUrl?: string): string | undefined {
  try {
    const decoded = decodeHTMLAttribute(href);
    let url: URL;
    try {
      url = new URL(decoded);
    } catch {
      if (!baseUrl) return undefined;
      const base = new URL(baseUrl);
      if (base.protocol !== "https:" && base.protocol !== "http:") return undefined;
      url = new URL(decoded, base);
    }
    return url.protocol === "https:" || url.protocol === "http:" ? url.href : undefined;
  } catch {
    return undefined;
  }
}

export function openMarkdownLink(event: MouseEvent, url: string, onopen: (url: string) => void | Promise<void>): void {
  event.preventDefault();
  event.stopPropagation();
  void onopen(url);
}
