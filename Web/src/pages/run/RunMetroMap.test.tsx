import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RunMetro } from "../../api/wire.gen";
import { RunMetroMap, type RunMetroMapProps } from "./RunMetroMap";

/** A hand-written metro: the trunk Triage, Spec (the run is here), a Quick build on a route not taken, and an approval. */
const fixture = {
  width: 420,
  height: 150,
  colour: 2,
  trigger: { text: "Sized", box: { x: 8, y: 32, width: 70, height: 16 } },
  segments: [
    { id: "line-0-0", state: "done", path: { subpaths: [[{ x: 78, y: 40 }, { x: 150, y: 40 }]], cornerRadius: 10 } },
    { id: "line-0-1", state: "todo", path: { subpaths: [[{ x: 150, y: 40 }, { x: 300, y: 40 }]], cornerRadius: 10 } },
    { id: "line-1-0", state: "skip", path: { subpaths: [[{ x: 170, y: 40 }, { x: 180, y: 104 }, { x: 260, y: 104 }]], cornerRadius: 10 } }
  ],
  stations: [
    {
      id: "triage", number: 1, name: "Triage", lines: ["Triage"], glyph: "circle", x: 78, y: 40,
      label: { x: 48, y: 50, width: 60, height: 13 }, hit: { x: 48, y: 31, width: 60, height: 32 },
      state: "done", status: "✓ 3m", look: "done", tagged: true, statusBox: { x: 40, y: 10, width: 76, height: 16 }
    },
    {
      id: "spec", number: 2, name: "Spec", lines: ["Spec"], glyph: "circle", x: 150, y: 40,
      label: { x: 126, y: 50, width: 48, height: 13 }, hit: { x: 126, y: 31, width: 48, height: 32 },
      state: "current", status: "RUNNING 2m", look: "running", tagged: true, statusBox: { x: 110, y: 10, width: 80, height: 16 }
    },
    {
      id: "quick-build", number: 3, name: "Quick build", lines: ["Quick build"], glyph: "square", x: 260, y: 104,
      label: { x: 220, y: 114, width: 80, height: 13 }, hit: { x: 220, y: 95, width: 80, height: 32 },
      state: "skip", status: "NOT TAKEN", look: "plain", tagged: false, statusBox: { x: 220, y: 74, width: 80, height: 16 }
    },
    {
      id: "approve", number: 4, name: "Approve", lines: ["Approve"], glyph: "diamond", x: 340, y: 40,
      label: { x: 316, y: 50, width: 48, height: 13 }, hit: { x: 316, y: 31, width: 48, height: 32 },
      state: "todo", status: "", look: "plain", tagged: false, statusBox: { x: 316, y: 10, width: 48, height: 16 }
    }
  ],
  routes: [
    { id: "size:complex", fork: "size", route: "complex", text: "Complex", x: 170, y: 33, width: 66, height: 14, taken: true, feeds: false },
    { id: "size:simple", fork: "size", route: "simple", text: "Simple", x: 190, y: 97, width: 56, height: 14, taken: false, feeds: false }
  ],
  ends: [{ x: 370, y: 40 }]
};

/** The fixture with one station changed, for the looks a current station can have. */
function withStation(id: string, patch: Record<string, unknown>) {
  return { ...fixture, stations: fixture.stations.map(station => station.id === id ? { ...station, ...patch } : station) };
}

function mount(overrides: Partial<RunMetroMapProps> = {}, source: unknown = fixture) {
  const metro = source as RunMetro;
  const props: RunMetroMapProps = {
    metro,
    metadata: "Belt v1 · 3 stages",
    caption: "Now at Spec",
    selectedStageId: null,
    onSelectStage: vi.fn(),
    ...overrides
  };
  render(<RunMetroMap {...props} />);
  return props;
}

/** Props for one station selected in the fixture, or with the current station's look changed. */
function mountPropsFor(id: string, look?: string): RunMetroMapProps {
  const source = look ? withStation("spec", { look }) : fixture;
  return { metro: source as RunMetro, metadata: "Belt v1", selectedStageId: id, onSelectStage: vi.fn() };
}

/** The glyph's class names for a station, as the run map draws them. */
function stopClass(id: string): string {
  return document.querySelector(`[data-stop="${id}"]`)?.getAttribute("class") ?? "";
}

afterEach(() => cleanup());

describe("RunMetroMap", () => {
  it("keeps the run page's card and header, with the metadata and caption over the line", () => {
    mount();
    const section = screen.getByRole("region", { name: "Stage line" });
    expect(section.className).toContain("run-line");
    expect(section.className).toContain("paper-card");
    expect(within(section).getByText("Belt v1 · 3 stages")).toBeTruthy();
    expect(within(section).getByText("Now at Spec")).toBeTruthy();
  });

  it("draws one button per station and reports the one picked", () => {
    const props = mount({ selectedStageId: "spec" });
    const stations = screen.getAllByRole("button");
    expect(stations.map(button => button.getAttribute("data-stage"))).toEqual(["triage", "spec", "quick-build", "approve"]);
    const spec = document.querySelector<HTMLButtonElement>('button[data-stage="spec"]')!;
    expect(spec.getAttribute("aria-pressed")).toBe("true");
    expect(spec.getAttribute("data-selected")).toBe("true");
    expect(spec.getAttribute("aria-label")).toBe("02. Spec — RUNNING 2m");
    fireEvent.click(document.querySelector<HTMLButtonElement>('button[data-stage="triage"]')!);
    expect(props.onSelectStage).toHaveBeenCalledWith("triage");
  });

  it("haloes the station the run is at, concentric with its glyph, in the status colour", () => {
    mount();
    const stop = document.querySelector('[data-stop="spec"]')!;
    const glyph = stop.querySelector(".run-metro-glyph")!;
    const halo = stop.querySelector(".run-metro-halo")!;
    expect(halo.getAttribute("class")).toContain("is-running");
    expect(halo.getAttribute("cx")).toBe(glyph.getAttribute("cx"));
    expect(halo.getAttribute("cy")).toBe(glyph.getAttribute("cy"));
    expect(stop.querySelector(".run-metro-pulse")).toBeTruthy();
    expect(document.querySelector('[data-stop="triage"] .run-metro-halo')).toBeNull();
    expect(document.querySelector(".run-metro-marker")).toBeNull();
    expect(screen.getByText("RUNNING 2m")).toBeTruthy();
    expect(document.querySelectorAll('[data-state="done"]')).toHaveLength(1);
    expect(document.querySelectorAll('[data-state="todo"]')).toHaveLength(1);
    expect(document.querySelectorAll('[data-state="skip"]')).toHaveLength(1);
    expect(document.querySelector('[data-stop="quick-build"]')?.getAttribute("class")).toContain("run-metro-stop-skip");
  });

  it("dims a route the run did not take and paints the belt's colour on the line", () => {
    mount();
    expect(document.querySelector('[data-route="size:simple"]')?.className).toContain("is-not-taken");
    expect(document.querySelector('[data-route="size:complex"]')?.className).not.toContain("is-not-taken");
    const done = document.querySelector<SVGPathElement>('[data-segment="line-0-0"]');
    expect(done?.getAttribute("style")).toContain("var(--metro-line-2)");
    expect(done?.getAttribute("d")).toMatch(/^M78 40/);
  });

  it("colours only a non-current selected station's glyph blue, for each glyph, and leaves its name and wash alone", () => {
    mount({ selectedStageId: "triage" });
    expect(stopClass("triage")).toContain("is-selected");
    expect(document.querySelector('[data-stop="triage"] .run-metro-glyph')).toBeTruthy();
    expect(document.querySelector('[data-stage="triage"]')?.getAttribute("aria-pressed")).toBe("true");
    expect(document.querySelector('[data-stage="triage"]')?.className).not.toContain("is-selected");
    expect(document.querySelector(".run-metro-name.is-selected")).toBeNull();
    expect(document.querySelector('[data-stage="spec"]')?.getAttribute("aria-pressed")).toBe("false");
    for (const id of ["quick-build", "approve"]) {
      cleanup();
      render(<RunMetroMap {...mountPropsFor(id)} />);
      expect(stopClass(id)).toContain("is-selected");
    }
  });

  it("keeps a current station's own look when it is selected, with its halo and pulse", () => {
    mount({ selectedStageId: "spec" });
    expect(stopClass("spec")).toContain("is-running");
    expect(stopClass("spec")).not.toContain("is-selected");
    expect(document.querySelector('[data-stop="spec"] .run-metro-halo')?.getAttribute("class")).toContain("is-running");
    expect(document.querySelector('[data-stop="spec"] .run-metro-pulse')).toBeTruthy();
  });

  it("keeps the waiting and held looks on a selected current station", () => {
    for (const look of ["waiting", "held"]) {
      cleanup();
      render(<RunMetroMap {...mountPropsFor("spec", look)} />);
      expect(stopClass("spec")).toContain(`is-${look}`);
      expect(stopClass("spec")).not.toContain("is-selected");
      expect(document.querySelector('[data-stop="spec"] .run-metro-pulse')).toBeTruthy();
    }
  });

  it("selects a station by click and by keyboard, and reports it", async () => {
    const user = userEvent.setup();
    const props = mount({ selectedStageId: null });
    await user.click(document.querySelector<HTMLButtonElement>('button[data-stage="quick-build"]')!);
    expect(props.onSelectStage).toHaveBeenLastCalledWith("quick-build");
    const approve = document.querySelector<HTMLButtonElement>('button[data-stage="approve"]')!;
    approve.focus();
    await user.keyboard("{Enter}");
    expect(props.onSelectStage).toHaveBeenLastCalledWith("approve");
    expect(approve).toBe(document.activeElement);
  });
});
