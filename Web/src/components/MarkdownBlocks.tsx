import type { ReactNode } from "react";
import type { MarkdownBlock } from "../api/wire.gen";

const headingTags = ["h1", "h2", "h3", "h4", "h5", "h6"] as const;
const safeSchemes = ["http", "https", "mailto"];

/**
 * The href for a link the daemon's Markdown wrote, or null when the link must stay text.
 * Only http:, https:, mailto: and same-origin relative references are clickable. Whitespace, control
 * characters and backslashes are refused outright, so obfuscated schemes never reach the href.
 */
export function safeHref(raw: string): string | null {
  if (!raw || /[\u0000- \u007f\\]/.test(raw)) return null;
  if (raw.startsWith("//")) return null;
  // Anything before a colon that comes before any path separator reads as a scheme, so it must be an allowed one.
  const colon = raw.indexOf(":");
  if (colon !== -1 && !raw.slice(0, colon).includes("/")) {
    return safeSchemes.includes(raw.slice(0, colon).toLowerCase()) ? raw : null;
  }
  return raw;
}

// Code spans first, then links, strong and emphasis. Matches stop at line ends, so a span never crosses a hard line break.
// Emphasis opens and closes only against non-space text, as in CommonMark, so "a * b * c" stays plain.
const inlinePattern = /`([^`\n]+)`|\[([^\]\n]+)\]\(([^)\s]+)\)|\*\*(?=\S)([^\n]*?\S)\*\*|\*(?=\S)([^*\n]*?\S)\*/g;

/**
 * Inline text as React nodes. Code spans are literal; links, strong and emphasis nest inside one
 * another. Raw HTML is never parsed, so it reaches the page as the text it was written as.
 */
export function renderInline(text: string, keyPrefix = "inline"): ReactNode[] {
  const nodes: ReactNode[] = [];
  let last = 0;
  let index = 0;
  for (const match of text.matchAll(inlinePattern)) {
    const start = match.index ?? 0;
    if (start > last) nodes.push(text.slice(last, start));
    const [whole, code, linkLabel, linkTarget, strong, emphasis] = match;
    const key = `${keyPrefix}-${index++}`;
    if (code !== undefined) nodes.push(<code key={key}>{code}</code>);
    else if (linkLabel !== undefined) {
      const href = safeHref(linkTarget ?? "");
      const label = renderInline(linkLabel, key);
      nodes.push(href ? <a key={key} href={href}>{label}</a> : <span key={key}>{label}</span>);
    } else if (strong !== undefined) nodes.push(<strong key={key}>{renderInline(strong, key)}</strong>);
    else if (emphasis !== undefined) nodes.push(<em key={key}>{renderInline(emphasis, key)}</em>);
    last = start + whole.length;
  }
  if (last < text.length) nodes.push(text.slice(last));
  return nodes;
}

function BlockView({ block }: { block: MarkdownBlock }) {
  if (block.type === "heading") {
    const Heading = headingTags[Math.min(Math.max(block.level, 1), 6) - 1]!;
    return <Heading className="markdown-heading">{renderInline(block.text)}</Heading>;
  }
  if (block.type === "item") {
    return <div className="markdown-item"><span className="markdown-marker">{block.marker}</span><span className="markdown-item-text">{renderInline(block.text)}</span></div>;
  }
  if (block.type === "code") return <pre className="markdown-code"><code>{block.text}</code></pre>;
  return <p className="markdown-paragraph">{renderInline(block.text)}</p>;
}

/** The daemon's ordered blocks, rendered in that order without any HTML injection. */
export function MarkdownBlocks({ blocks }: { blocks: readonly MarkdownBlock[] }) {
  return <div className="markdown-blocks">{blocks.map((block, index) => <BlockView key={index} block={block} />)}</div>;
}
