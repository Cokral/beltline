import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";

export interface SheetProps {
  title: string;
  open: boolean;
  /** Called for Escape and the header Close button. Footer buttons such as Cancel call it too. */
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
  /** The app's form sheets: a tag over the title on paper, no header bar or Close button, at this width. */
  tag?: string;
  width?: number;
  /** A line under the title, such as what the sheet acts on. */
  subtitle?: ReactNode;
  /** A form sheet that still closes from its header, with the app's small × button. */
  closeMark?: boolean;
  /** Native confirmations use their footer Cancel action instead of a header Close button. */
  hideClose?: boolean;
  /** Extra class names on the panel. */
  className?: string;
}

const focusableSelector = [
  "a[href]", "button:not([disabled])", "input:not([disabled]):not([type=hidden])", "select:not([disabled])",
  "textarea:not([disabled])", "[tabindex]:not([tabindex='-1'])"
].join(",");

function focusables(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(focusableSelector));
}

/**
 * A modal sheet. Nothing is rendered while it is closed, so opening and cancelling it sends no command.
 * While open, focus starts inside, Tab and Shift+Tab cycle within it, the rest of the page is inert, and
 * closing returns focus to the element that opened it when that element still exists.
 */
export function Sheet({ open, ...props }: SheetProps) {
  if (!open) return null;
  return <SheetDialog {...props} />;
}

function SheetDialog({ title, onClose, children, footer, tag, width, subtitle, closeMark, hideClose, className }: Omit<SheetProps, "open">) {
  const titleId = useId();
  const dialogRef = useRef<HTMLDivElement>(null);
  const [opener] = useState(() => (document.activeElement instanceof HTMLElement ? document.activeElement : null));

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const layer = dialog.parentElement;
    const inerted: HTMLElement[] = [];
    for (const child of Array.from(document.body.children)) {
      if (child === layer || !(child instanceof HTMLElement) || child.hasAttribute("inert")) continue;
      child.setAttribute("inert", "");
      inerted.push(child);
    }
    // Focus starts on the first control of the content, then the header's Close, then the sheet itself.
    const body = dialog.querySelector<HTMLElement>(".sheet-body") ?? dialog;
    (focusables(body)[0] ?? focusables(dialog)[0] ?? dialog).focus();
    // Focus that reaches the page by any other route (a script, a scroll-into-view) is brought back into the sheet.
    const pullBack = (event: FocusEvent) => {
      if (event.target instanceof Node && !dialog.contains(event.target)) (focusables(dialog)[0] ?? dialog).focus();
    };
    document.addEventListener("focusin", pullBack);
    return () => {
      document.removeEventListener("focusin", pullBack);
      for (const element of inerted) element.removeAttribute("inert");
      if (opener?.isConnected) opener.focus();
    };
  }, [opener]);

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Escape") {
      event.preventDefault();
      onClose();
      return;
    }
    if (event.key !== "Tab") return;
    const dialog = dialogRef.current;
    if (!dialog) return;
    const items = focusables(dialog);
    const first = items[0] ?? dialog;
    const last = items[items.length - 1] ?? dialog;
    const active = document.activeElement;
    if (event.shiftKey && (active === first || active === dialog)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && (active === last || active === dialog)) {
      event.preventDefault();
      first.focus();
    }
  };

  return createPortal(
    <div className="sheet-layer" data-sheet-layer onClick={event => { if (event.target === event.currentTarget) onClose(); }}>
      <SheetPanel title={title} titleId={titleId} panelRef={dialogRef} onKeyDown={onKeyDown} onClose={hideClose || (tag && !closeMark) ? undefined : onClose} footer={footer} tag={tag} width={width}
        subtitle={subtitle} closeMark={closeMark} className={className}>{children}</SheetPanel>
    </div>,
    document.body
  );
}

export interface SheetPanelProps {
  title: string;
  titleId: string;
  onClose?: () => void;
  footer?: ReactNode;
  children: ReactNode;
  panelRef?: RefObject<HTMLDivElement | null>;
  onKeyDown?: (event: KeyboardEvent<HTMLDivElement>) => void;
  tag?: string;
  width?: number;
  subtitle?: ReactNode;
  closeMark?: boolean;
  className?: string;
}

/** The sheet's surface and layout without its modal behaviour, so a static sample can show the same panel. */
export function SheetPanel({ title, titleId, onClose, footer, children, panelRef, onKeyDown, tag, width, subtitle, closeMark, className }: SheetPanelProps) {
  const classes = ["sheet", tag ? "is-form" : null, className ?? null].filter(Boolean).join(" ");
  return <div ref={panelRef} role="dialog" aria-modal="true" aria-labelledby={titleId} className={classes} tabIndex={-1} onKeyDown={onKeyDown}
    style={width ? { width: `min(${width}px, 100%)` } : undefined}>
    <header className="sheet-header">
      {tag ? <div className="form-heading"><span className="paper-tag">{tag}</span><h2 id={titleId}>{title}</h2>{subtitle && <p className="sheet-subtitle">{subtitle}</p>}</div> : <h2 id={titleId}>{title}</h2>}
      {onClose && (closeMark
        ? <button type="button" className="paper-button paper-button--small sheet-close-mark" aria-label="Close" onClick={onClose}>×</button>
        : <button type="button" className="quiet-button" onClick={onClose}>Close</button>)}
    </header>
    <div className="sheet-body">{children}</div>
    {footer && <footer className="sheet-footer">{footer}</footer>}
  </div>;
}
