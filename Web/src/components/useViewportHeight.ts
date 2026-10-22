import { useLayoutEffect, useRef, useState, type RefObject } from "react";

/**
 * The height of the scroll viewport an element is shown in (the shell's `main.content`), measured before paint and
 * kept current as it resizes, so a column can fit the visible area while the page scrolls. It follows the viewport's
 * own box, so a status message above the content shrinks it. It is 0 until the viewport is found, so a caller that
 * needs a fallback applies it itself.
 */
export function useViewportHeight<Element extends HTMLElement>(): [RefObject<Element | null>, number] {
  const ref = useRef<Element | null>(null);
  const [height, setHeight] = useState(0);
  useLayoutEffect(() => {
    const viewport = ref.current?.closest<HTMLElement>(".content");
    if (!viewport) return;
    const measure = () => setHeight(viewport.clientHeight);
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    observer.observe(viewport);
    return () => observer.disconnect();
  }, []);
  return [ref, height];
}
