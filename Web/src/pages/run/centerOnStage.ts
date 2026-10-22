import { useLayoutEffect, useRef, type RefObject } from "react";

/** The scroll position that puts a point in the middle of a viewport, held between the start and the end of the content. */
export function centredScrollLeft(point: number, viewportWidth: number, contentWidth: number): number {
  const furthest = Math.max(0, contentWidth - viewportWidth);
  return Math.min(furthest, Math.max(0, point - viewportWidth / 2));
}

/**
 * Opens a run's stage line with its current stage in the middle of the horizontal scroll, once per run, when the stage is
 * out of view. The position clamps to the start or the end when the stage sits too near either. The stage selected in the URL does not move it,
 * so a historical stage can be shown without re-centring.
 */
export function useCentreOnStage(section: RefObject<HTMLElement | null>, opening: { run: string; stage: string } | undefined): void {
  const centred = useRef<string | null>(null);
  const run = opening?.run;
  const stage = opening?.stage;
  useLayoutEffect(() => {
    const viewport = section.current?.querySelector<HTMLElement>("[data-scroll-viewport]");
    if (!viewport || run === undefined || stage === undefined || centred.current === run) return;
    const target = Array.from(viewport.querySelectorAll<HTMLElement>("[data-stage]")).find(node => node.dataset.stage === stage);
    if (!target) return;
    const viewportBox = viewport.getBoundingClientRect();
    const targetBox = target.getBoundingClientRect();
    // A stage already in view stays where the scroll put it, so the belt's start is not pushed out of sight.
    if (targetBox.left >= viewportBox.left && targetBox.right <= viewportBox.right) {
      centred.current = run;
      return;
    }
    const point = targetBox.left - viewportBox.left + viewport.scrollLeft + targetBox.width / 2;
    viewport.scrollLeft = centredScrollLeft(point, viewport.clientWidth, viewport.scrollWidth);
    centred.current = run;
  }, [section, run, stage]);
}
