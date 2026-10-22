import { useEffect, useRef, type ReactNode } from "react";

export interface RunMenuProps {
  /** The trigger's accessible name; the card is announced as "<label> menu". */
  label: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}

/**
 * The Run page's one disclosure: a round three-dot trigger that opens the run's controls and its open links.
 * The card floats over the page, so opening it reflows nothing. Escape closes it and returns focus to the
 * trigger; the page's scrim consumes an outside click, so the page underneath never acts.
 */
export function RunMenu({ label, open, onOpenChange, children }: RunMenuProps) {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const mounted = useRef(false);
  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      triggerRef.current?.focus();
      onOpenChange(false);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [open, onOpenChange]);
  // An outside click lands on the page's scrim, which cannot take focus, so focus would otherwise fall to
  // the document body when the card closes. A menu that replaced this one keeps its own focus.
  useEffect(() => {
    if (!mounted.current) { mounted.current = true; return; }
    if (open) return;
    const active = document.activeElement;
    if (active === null || active === document.body) triggerRef.current?.focus();
  }, [open]);
  return <div className="run-menu">
    <button ref={triggerRef} type="button" className="run-menu-trigger" aria-label={label} title={label} aria-haspopup="true" aria-expanded={open} onClick={() => onOpenChange(!open)}>
      <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true" fill="currentColor"><circle cx="3" cy="7" r="1.25" /><circle cx="7" cy="7" r="1.25" /><circle cx="11" cy="7" r="1.25" /></svg>
    </button>
    {open && <div className="run-menu-card" aria-label={`${label} menu`}>{children}</div>}
  </div>;
}
