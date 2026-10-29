<script lang="ts">
  import { decodeHTML } from "entities";
  import type { Token } from "marked";
  import { markdownLinkUrl, openMarkdownLink, parseMarkdown } from "$lib/markdown";

  let { source, baseUrl, onopen } = $props<{
    source: string;
    baseUrl?: string;
    onopen: (url: string) => void | Promise<void>;
  }>();

  const tokens = $derived(parseMarkdown(source));
</script>

{#snippet content(items: Token[] = [])}
  {#each items as token}
    {#if token.type === "heading"}
      <svelte:element this={`h${token.depth}`}>{@render content(token.tokens)}</svelte:element>
    {:else if token.type === "paragraph"}
      <p>{@render content(token.tokens)}</p>
    {:else if token.type === "list"}
      <svelte:element this={token.ordered ? "ol" : "ul"} start={token.ordered ? token.start : undefined}>
        {#each token.items as item}
          <li class:task={item.task}>{@render content(item.tokens)}</li>
        {/each}
      </svelte:element>
    {:else if token.type === "blockquote"}
      <blockquote>{@render content(token.tokens)}</blockquote>
    {:else if token.type === "code"}
      <pre><code>{token.text}</code></pre>
    {:else if token.type === "codespan"}
      <code>{token.text}</code>
    {:else if token.type === "strong" || token.type === "em" || token.type === "del"}
      <svelte:element this={token.type}>{@render content(token.tokens)}</svelte:element>
    {:else if token.type === "link"}
      {@const url = markdownLinkUrl(token.href, baseUrl)}
      {#if url}
        <a href={url} title={token.title || undefined} rel="noopener noreferrer" onclick={(event) => openMarkdownLink(event, url, onopen)}>{@render content(token.tokens)}</a>
      {:else}
        {@render content(token.tokens)}
      {/if}
    {:else if token.type === "image"}
      {decodeHTML(token.text)}
    {:else if token.type === "html"}
      <span class="raw-html">{token.text}</span>
    {:else if token.type === "text" && token.tokens}
      {@render content(token.tokens)}
    {:else if token.type === "text"}
      {token.escaped ? token.text : decodeHTML(token.text)}
    {:else if token.type === "escape"}
      {token.text}
    {:else if token.type === "br"}
      <br />
    {:else if token.type === "hr"}
      <hr />
    {:else if token.type === "checkbox"}
      <input type="checkbox" checked={token.checked} disabled aria-label={token.checked ? "已完成" : "未完成"} />
    {:else if token.type === "table"}
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              {#each token.header as cell}
                <th style:text-align={cell.align || undefined}>{@render content(cell.tokens)}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each token.rows as row}
              <tr>
                {#each row as cell}
                  <td style:text-align={cell.align || undefined}>{@render content(cell.tokens)}</td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else if token.type !== "space" && token.type !== "def"}
      {token.raw}
    {/if}
  {/each}
{/snippet}

<div class="markdown text-sm leading-relaxed text-slate-700 dark:text-zinc-300">
  {@render content(tokens)}
</div>

<style>
  .markdown {
    overflow-wrap: anywhere;
  }

  .markdown :global(h1),
  .markdown :global(h2),
  .markdown :global(h3),
  .markdown :global(h4),
  .markdown :global(h5),
  .markdown :global(h6) {
    margin: 1.25em 0 0.5em;
    font-weight: 700;
    line-height: 1.4;
  }

  .markdown :global(h1) { font-size: 1.4em; }
  .markdown :global(h2) { font-size: 1.2em; }
  .markdown :global(h3) { font-size: 1.1em; }
  .markdown :global(p),
  .markdown :global(ul),
  .markdown :global(ol),
  .markdown :global(blockquote),
  .markdown :global(pre),
  .markdown :global(.table-scroll) {
    margin: 0.75em 0;
  }

  .markdown :global(ul) { list-style: disc; padding-left: 1.5em; }
  .markdown :global(ol) { list-style: decimal; padding-left: 1.5em; }
  .markdown :global(li) { margin: 0.25em 0; }
  .markdown :global(li > p) { margin: 0.25em 0; }
  .markdown :global(li.task) { list-style: none; }
  .markdown :global(input) { margin-right: 0.4em; vertical-align: middle; }

  .markdown :global(blockquote) {
    border-left: 3px solid color-mix(in srgb, currentColor 30%, transparent);
    padding-left: 0.9em;
    color: color-mix(in srgb, currentColor 80%, transparent);
  }

  .markdown :global(code) {
    border-radius: 0.25rem;
    background: color-mix(in srgb, currentColor 8%, transparent);
    padding: 0.1em 0.3em;
    font-family: var(--font-mono);
    font-size: 0.85em;
  }

  .markdown :global(pre) {
    overflow-x: auto;
    border: 1px solid color-mix(in srgb, currentColor 20%, transparent);
    border-radius: 0.5rem;
    background: color-mix(in srgb, currentColor 5%, transparent);
    padding: 0.75em;
    white-space: pre;
    overflow-wrap: normal;
  }

  .markdown :global(pre code) { background: none; padding: 0; }
  .markdown :global(a) { text-decoration: underline; text-underline-offset: 0.2em; }
  .markdown :global(a:hover) { opacity: 0.8; }
  .markdown :global(a:focus-visible) { outline: 2px solid currentColor; outline-offset: 2px; }
  .markdown :global(hr) { margin: 1em 0; border-color: color-mix(in srgb, currentColor 20%, transparent); }
  .markdown :global(.raw-html) { white-space: pre-wrap; }
  .markdown :global(.table-scroll) { overflow-x: auto; }
  .markdown :global(table) { width: 100%; border-collapse: collapse; }
  .markdown :global(th),
  .markdown :global(td) { border: 1px solid color-mix(in srgb, currentColor 20%, transparent); padding: 0.4em 0.65em; }
  .markdown :global(th) { background: color-mix(in srgb, currentColor 5%, transparent); }
  .markdown > :global(:first-child) { margin-top: 0; }
  .markdown > :global(:last-child) { margin-bottom: 0; }
</style>
