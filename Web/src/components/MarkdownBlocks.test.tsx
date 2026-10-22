import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { AttemptOutputResponse, MarkdownBlock } from "../api/wire.gen";
import { golden } from "../test/golden";
import { MarkdownBlocks, renderInline, safeHref } from "./MarkdownBlocks";

const outputOf = (name: string) => golden<AttemptOutputResponse>(name);

const paragraph = (text: string): MarkdownBlock => ({ type: "paragraph", text });

function renderText(text: string) {
  return render(<MarkdownBlocks blocks={[paragraph(text)]} />);
}

describe("block rendering", () => {
  it("renders every block kind from the long output golden in its order", () => {
    const { container } = render(<MarkdownBlocks blocks={outputOf("run-attempt-output-long.json").blocks} />);
    const root = container.querySelector(".markdown-blocks")!;
    expect(Array.from(root.children).map(child => child.tagName)).toEqual(["H1", "P", "DIV", "DIV", "DIV", "PRE", "PRE"]);
    expect(container.querySelector("h1")?.textContent).toBe("Release notes");
    const markers = Array.from(container.querySelectorAll(".markdown-marker")).map(marker => marker.textContent);
    expect(markers).toEqual(["•", "•", "1."]);
    const items = Array.from(container.querySelectorAll(".markdown-item-text")).map(item => item.textContent);
    expect(items).toEqual(["first item", "second item with a wrapped\ncontinuation that remains part of the same item", "numbered item"]);
  });

  it("keeps code blocks literal and preserves their line breaks and trailing spaces", () => {
    const { container } = render(<MarkdownBlocks blocks={outputOf("run-attempt-output-long.json").blocks} />);
    const codes = Array.from(container.querySelectorAll("pre code")).map(code => code.textContent);
    expect(codes).toEqual(['let greeting = "hello  "\nprint(greeting)', "unclosed code keeps trailing spaces"]);
  });

  it("renders the short output golden's paragraph", () => {
    const { container } = render(<MarkdownBlocks blocks={outputOf("run-attempt-output.json").blocks} />);
    expect(container.querySelector("p.markdown-paragraph")?.textContent).toBe("Contract stage output");
  });

  it("keeps the daemon's heading level and clamps levels beyond six to h6", () => {
    const { container } = render(<MarkdownBlocks blocks={[
      { type: "heading", level: 1, text: "One" },
      { type: "heading", level: 3, text: "Three" },
      { type: "heading", level: 9, text: "Nine" }
    ]} />);
    expect(Array.from(container.querySelectorAll("h1, h2, h3, h4, h5, h6")).map(heading => heading.tagName)).toEqual(["H1", "H3", "H6"]);
  });
});

describe("inline text", () => {
  it("renders emphasis, strong text, code and links inside a paragraph", () => {
    const { container } = renderText("A *soft* and **bold** word, `inline()` and [docs](/docs).");
    expect(container.querySelector("em")?.textContent).toBe("soft");
    expect(container.querySelector("strong")?.textContent).toBe("bold");
    expect(container.querySelector("code")?.textContent).toBe("inline()");
    expect(container.querySelector("a")?.getAttribute("href")).toBe("/docs");
  });

  it("nests emphasis in strong text and a link in strong text", () => {
    const { container } = renderText("**strong *and emphasis* [link](https://example.com/x)**");
    const strong = container.querySelector("strong")!;
    expect(strong.querySelector("em")?.textContent).toBe("and emphasis");
    expect(strong.querySelector("a")?.getAttribute("href")).toBe("https://example.com/x");
  });

  it("keeps code spans literal, with no emphasis or links inside them", () => {
    const { container } = renderText("`**not bold** [not](https://example.com)`");
    expect(container.querySelector("code")?.textContent).toBe("**not bold** [not](https://example.com)");
    expect(container.querySelector("strong, a")).toBeNull();
  });

  it("keeps code block text literal", () => {
    const { container } = render(<MarkdownBlocks blocks={[{ type: "code", text: "**x** <b>y</b> [z](javascript:alert(1))" }]} />);
    expect(container.querySelector("pre code")?.textContent).toBe("**x** <b>y</b> [z](javascript:alert(1))");
    expect(container.querySelector("b, a, strong")).toBeNull();
  });

  it("keeps an unclosed marker as plain text", () => {
    const { container } = renderText("a * lone star and a ** pair");
    expect(container.textContent).toBe("a * lone star and a ** pair");
    expect(container.querySelector("em, strong")).toBeNull();
  });
});

describe("safe links", () => {
  it.each([
    ["https://example.com/a?b=c#d", "https://example.com/a?b=c#d"],
    ["HTTPS://EXAMPLE.com", "HTTPS://EXAMPLE.com"],
    ["http://localhost:7331/x", "http://localhost:7331/x"],
    ["mailto:team@example.com", "mailto:team@example.com"],
    ["MAILTO:team@example.com", "MAILTO:team@example.com"],
    ["/runs/issues%3Aacme%2Fweb%2361", "/runs/issues%3Aacme%2Fweb%2361"],
    ["#section", "#section"],
    ["docs/notes.md", "docs/notes.md"],
    ["?page=2", "?page=2"]
  ])("keeps %s clickable", (target, href) => {
    expect(safeHref(target)).toBe(href);
    const { container } = renderText(`[label](${target})`);
    expect(container.querySelector("a")?.getAttribute("href")).toBe(href);
  });

  it.each([
    "javascript:alert(1)",
    "JaVaScRiPt:alert(1)",
    "javascript:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "DATA:text/html;base64,PHNjcmlwdD4=",
    "file:///etc/passwd",
    "vbscript:msgbox(1)",
    "//evil.example/path",
    "/\\evil.example",
    "&#106;avascript:alert(1)",
    "%6Aavascript:alert(1)",
    "ftp://example.com/file",
    "java\tscript:alert(1)",
    "javascript\u0000:alert(1)"
  ])("keeps %j as readable text, not a link", target => {
    expect(safeHref(target)).toBeNull();
    const { container } = renderText(`[readable label](${target})`);
    expect(container.querySelector("a")).toBeNull();
    expect(container.textContent).toContain("readable label");
  });

  it("renders a link target that contains a space as text, not as a link", () => {
    const { container } = renderText("[label](javascript: alert(1))");
    expect(container.querySelector("a")).toBeNull();
    expect(container.textContent).toBe("[label](javascript: alert(1))");
  });

  it("never injects raw HTML: markup stays visible as text", () => {
    const { container } = renderText('<img src="x" onerror="alert(1)"> <script>alert(2)</script> <a href="javascript:alert(3)">hi</a>');
    expect(container.querySelector("img, script, a")).toBeNull();
    expect(container.textContent).toBe('<img src="x" onerror="alert(1)"> <script>alert(2)</script> <a href="javascript:alert(3)">hi</a>');
  });

  it("renderInline returns nodes, not markup strings", () => {
    const { container } = render(<p>{renderInline("plain **bold**")}</p>);
    expect(container.innerHTML).toBe("<p>plain <strong>bold</strong></p>");
  });
});
