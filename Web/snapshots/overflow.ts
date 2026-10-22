export type OverflowFinding = { column: string; element: string; excess: number };

export function assertNoColumnOverflows(caseId: string, size: { width: number; height: number }, findings: OverflowFinding[]): void {
  const first = findings[0];
  if (first) throw new Error(`${caseId} at ${size.width}x${size.height}: column ${first.column}, ${first.element} exceeds right edge by ${first.excess.toFixed(2)}px`);
}

export function findColumnOverflows(root: ParentNode = document): OverflowFinding[] {
  const description = (element: Element): string => {
    const tag = element.tagName.toLowerCase();
    const id = element.id ? `#${element.id}` : "";
    const classes = typeof element.className === "string" && element.className.trim()
      ? `.${element.className.trim().split(/\s+/).join(".")}` : "";
    return `${tag}${id}${classes}`;
  };
  const isVisible = (element: Element): boolean => {
    let current: Element | null = element;
    while (current) {
      const style = getComputedStyle(current);
      if (style.display === "none" || style.visibility === "hidden" || (style.opacity !== "" && Number(style.opacity) === 0)) return false;
      // Screen-reader-only text is clipped to nothing, so none of it is drawn.
      const clip = style.clip || (current as HTMLElement).style?.clip || "";
      if (current.classList.contains("screen-reader-only") || (style.position === "absolute" && /^rect\(0(px)?,? 0(px)?,? 0(px)?,? 0(px)?\)$/.test(clip))) return false;
      current = current.parentElement;
    }
    return true;
  };
  const findings: OverflowFinding[] = [];
  for (const column of root.querySelectorAll<HTMLElement>("[data-column]")) {
    const right = column.getBoundingClientRect().right;
    const name = column.dataset.column || "unnamed";
    const inspect = (element: Element, edge: number) => {
      const nearest = element.closest("[data-column]");
      if ((nearest !== column && element !== nearest) || !isVisible(element)) return;
      // Content inside an intentional scroll viewport scrolls within it, so it may run past the viewport's visible
      // edge up to its scroll width. The viewport itself still has to fit the column.
      const viewport = element.parentElement?.closest<HTMLElement>("[data-scroll-viewport]");
      const limit = viewport && column.contains(viewport) ? viewport.getBoundingClientRect().left + viewport.scrollWidth : right;
      const excess = edge - limit;
      if (excess > 1) findings.push({ column: name, element: description(element), excess });
    };
    for (const element of column.querySelectorAll<HTMLElement>("*")) {
      const rect = element.getBoundingClientRect();
      inspect(element, rect.right);
    }
    const walker = document.createTreeWalker(column, NodeFilter.SHOW_TEXT);
    let node: Node | null;
    while ((node = walker.nextNode())) {
      if (!node.textContent?.trim()) continue;
      const range = document.createRange();
      range.selectNodeContents(node);
      const owner = node.parentElement;
      if (!owner) continue;
      // Text an element clips with an ellipsis is drawn only up to that element's edge.
      const style = getComputedStyle(owner);
      const clipped = style.textOverflow === "ellipsis" && style.overflowX !== "visible";
      const ownerRight = owner.getBoundingClientRect().right;
      for (const rect of Array.from(range.getClientRects())) inspect(owner, clipped ? Math.min(rect.right, ownerRight) : rect.right);
    }
  }
  return findings;
}
