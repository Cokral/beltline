import { useEffect, useRef, useState, type RefObject } from "react";

/**
 * The laid-out width of an element, measured on mount and kept current as it resizes. It is 0 until the element is
 * mounted, so a caller that needs a fallback width applies it itself. `settleMs` waits for a pause in resizing before
 * measuring again, so a read keyed by the width asks the daemon once per settled width; 0 measures on every resize.
 */
export function useElementWidth<Element extends HTMLElement>(settleMs = 0): [RefObject<Element | null>, number] {
  const ref = useRef<Element | null>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const measure = () => setWidth(Math.round(element.getBoundingClientRect().width));
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      if (settleMs === 0) {
        measure();
        return;
      }
      clearTimeout(timer);
      timer = setTimeout(measure, settleMs);
    });
    observer.observe(element);
    return () => {
      clearTimeout(timer);
      observer.disconnect();
    };
  }, [settleMs]);
  return [ref, width];
}
