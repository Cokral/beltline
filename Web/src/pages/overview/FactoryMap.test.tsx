import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { OverviewResponse, OverviewRun } from "../../api/wire.gen";
import { FactoryMap, type FactoryMapProps } from "./FactoryMap";
import { hoveredAfter, resolveFocus, validPin } from "./focus";
import { golden } from "../../test/golden";

type Floor = OverviewResponse["floor"];
/** The machines look's Overview: its floor carries the machines and their drawing. */
type MachinesOverview = OverviewResponse & { floor: Floor & { machines: NonNullable<Floor["machines"]>; layout: NonNullable<Floor["layout"]> } };
const overview = (): MachinesOverview => golden<MachinesOverview>("views-overview-machines.json");

function runsOf(view: OverviewResponse): Map<string, OverviewRun> {
  return new Map(view.runs.map(run => [run.id, run]));
}

function handlers(): Omit<FactoryMapProps, "layout" | "belts" | "machines" | "runs" | "focus" | "pinned"> {
  return {
    onHover: vi.fn(),
    onPin: vi.fn(),
    onOpenRun: vi.fn(),
    onOpenMachine: vi.fn(),
    onOpenBelt: vi.fn()
  };
}

/** The page's own hover rule, so the delayed exit and the pin precedence run the way the Overview runs them. */
function Harness({ view, pinned: initialPin, initialHover = null, onHover }: {
  view: MachinesOverview;
  pinned: string | null;
  initialHover?: string | null;
  onHover?: (belt: string, pointing: boolean) => void;
}) {
  const [pinned, setPinned] = useState<string | null>(validPin(initialPin, view.floor.belts));
  const [hovered, setHovered] = useState<string | null>(initialHover);
  const focus = resolveFocus(pinned, hovered);
  return <>
    <button type="button" onClick={() => setPinned(current => (current === "docs" ? null : "docs"))}>pin docs</button>
    <FactoryMap layout={view.floor.layout} belts={view.floor.belts} machines={view.floor.machines} runs={runsOf(view)} focus={focus}
      pinned={pinned} onHover={(belt, pointing) => { onHover?.(belt, pointing); setHovered(current => hoveredAfter(current, belt, pointing)); }}
      onPin={belt => setPinned(current => (current === belt ? null : belt))} onOpenRun={vi.fn()} onOpenMachine={vi.fn()} onOpenBelt={vi.fn()} />
  </>;
}

const dimmed = (element: Element | null) => element?.getAttribute("style")?.includes("opacity: 0.35") ?? false;

afterEach(() => cleanup());

describe("Floor drawing", () => {
  it("names every entrance and machine, and shows the MIXED tag with the workers it was given", () => {
    const view = overview();
    render(<FactoryMap layout={view.floor.layout} belts={view.floor.belts} machines={view.floor.machines} runs={runsOf(view)} focus={null} pinned={null} {...handlers()} />);
    for (const belt of view.floor.belts) expect(screen.getByText(belt.name)).toBeTruthy();
    for (const machine of view.floor.machines) expect(document.querySelector(`[data-machine='${machine.id}']`)).not.toBeNull();
    const mixed = view.floor.machines.find(machine => machine.mixed);
    if (mixed) expect(screen.getAllByText("MIXED").some(tag => tag.getAttribute("title") === mixed.workers)).toBe(true);
  });

  it("shows up to five chips, and above five, four chips and the remaining count", () => {
    const view = overview();
    const machine = structuredClone(view.floor.machines[0]!);
    machine.chips = Array.from({ length: 4 }, (_, index) => ({ runId: `run-${index}`, ref: `#${index}`, state: "running", paused: false }));
    machine.more = 2;
    machine.runCount = 6;
    const crowded = { ...view, floor: { ...view.floor, machines: [machine, ...view.floor.machines.slice(1)] } };
    render(<FactoryMap layout={crowded.floor.layout} belts={crowded.floor.belts} machines={crowded.floor.machines} runs={runsOf(crowded)} focus={null} pinned={null} {...handlers()} />);
    expect(screen.getByText("+2")).toBeTruthy();
    expect(document.querySelector(`[data-machine='${machine.id}'] .factory-chips`)!.querySelectorAll(".overview-chip").length).toBe(4);
  });
});

describe("Floor hover and pin", () => {
  it("reports an entrance's enter and leave to the page", async () => {
    const user = userEvent.setup();
    const view = overview();
    const hover = vi.fn();
    render(<FactoryMap layout={view.floor.layout} belts={view.floor.belts} machines={view.floor.machines} runs={runsOf(view)} focus={null} pinned={null} {...handlers()} onHover={hover} />);
    const entrance = document.querySelector(".overview-entrance[data-belt='issues']")!;
    await user.hover(entrance);
    expect(hover).toHaveBeenLastCalledWith("issues", true);
    await user.unhover(entrance);
    expect(hover).toHaveBeenLastCalledWith("issues", false);
  });

  it("keeps a hover while the focus is none, and dims only unrelated machines when a belt is hovered", () => {
    const view = overview();
    render(<Harness view={view} pinned={null} initialHover="issues" />);
    const focusedMachines = view.floor.machines.filter(machine => machine.belts.includes("issues"));
    const unrelated = view.floor.machines.filter(machine => !machine.belts.includes("issues"));
    expect(document.querySelectorAll(".factory-track-focus").length).toBe(view.floor.layout.tracks.filter(stroke => stroke.belts.includes("issues")).length);
    for (const machine of focusedMachines) expect(dimmed(document.querySelector(`[data-machine='${machine.id}'] .factory-machine-body`))).toBe(false);
    for (const machine of unrelated) expect(dimmed(document.querySelector(`[data-machine='${machine.id}'] .factory-machine-body`))).toBe(true);
    for (const lane of document.querySelectorAll<SVGGElement>(".factory-lane")) {
      expect(lane.getAttribute("opacity") === "0.35" || lane.getAttribute("opacity") === "1").toBe(true);
    }
  });

  it("ignores a delayed exit from an earlier belt once another belt is hovered", () => {
    const view = overview();
    render(<Harness view={view} pinned={null} />);
    const issues = document.querySelector(".overview-entrance[data-belt='issues']")!;
    const docs = document.querySelector(".overview-entrance[data-belt='docs']")!;
    fireEvent.pointerEnter(issues);
    fireEvent.pointerEnter(docs);
    fireEvent.pointerLeave(issues);
    expect(document.querySelectorAll(".factory-track-focus").length).toBe(view.floor.layout.tracks.filter(stroke => stroke.belts.includes("docs")).length);
  });

  it("lets a valid pin take precedence over another hover, and restores the hover when unpinned", async () => {
    const user = userEvent.setup();
    const view = overview();
    render(<Harness view={view} pinned="issues" />);
    const docs = document.querySelector(".overview-entrance[data-belt='docs']")!;
    fireEvent.pointerEnter(docs);
    const pinnedTracks = view.floor.layout.tracks.filter(stroke => stroke.belts.includes("issues")).length;
    expect(document.querySelectorAll(".factory-track-focus").length).toBe(pinnedTracks);
    await user.click(screen.getByRole("button", { name: "pin docs" }));
    expect(document.querySelectorAll(".factory-track-focus").length).toBe(view.floor.layout.tracks.filter(stroke => stroke.belts.includes("docs")).length);
  });
});
