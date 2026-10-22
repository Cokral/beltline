import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useElementWidth } from "./useElementWidth";

let laidOutWidth = 0;
let fireResize: () => void = () => undefined;

class FakeResizeObserver {
  constructor(callback: () => void) { fireResize = callback; }
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

function Probe({ settleMs }: { settleMs?: number }) {
  const [ref, width] = useElementWidth<HTMLDivElement>(settleMs);
  return <div ref={ref}><output>{width}</output></div>;
}

describe("useElementWidth", () => {
  beforeEach(() => {
    laidOutWidth = 640.4;
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(() => ({ width: laidOutWidth }) as DOMRect);
    vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("reports the rounded laid-out width on mount", () => {
    render(<Probe />);
    expect(document.querySelector("output")?.textContent).toBe("640");
  });

  it("measures again on every resize when no settle delay is given", () => {
    render(<Probe />);
    laidOutWidth = 800;
    act(() => fireResize());
    expect(document.querySelector("output")?.textContent).toBe("800");
  });

  it("waits for the settle delay to pass before measuring a resize", () => {
    vi.useFakeTimers();
    render(<Probe settleMs={150} />);
    laidOutWidth = 800;
    act(() => fireResize());
    expect(document.querySelector("output")?.textContent).toBe("640");
    act(() => vi.advanceTimersByTime(149));
    expect(document.querySelector("output")?.textContent).toBe("640");
    act(() => vi.advanceTimersByTime(1));
    expect(document.querySelector("output")?.textContent).toBe("800");
  });

  it("restarts the settle delay on each resize, so only the settled width is measured", () => {
    vi.useFakeTimers();
    render(<Probe settleMs={150} />);
    laidOutWidth = 700;
    act(() => fireResize());
    act(() => vi.advanceTimersByTime(100));
    laidOutWidth = 900;
    act(() => fireResize());
    act(() => vi.advanceTimersByTime(100));
    expect(document.querySelector("output")?.textContent).toBe("640");
    act(() => vi.advanceTimersByTime(50));
    expect(document.querySelector("output")?.textContent).toBe("900");
  });
});
