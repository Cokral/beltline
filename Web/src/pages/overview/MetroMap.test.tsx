import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { OverviewResponse, OverviewRun } from "../../api/wire.gen";
import { MetroMap, type MetroMapProps } from "./MetroMap";
import { golden } from "../../test/golden";

/** The metro look's Overview: its response carries the metro's drawing. */
type MetroOverview = OverviewResponse & { metroLayout: NonNullable<OverviewResponse["metroLayout"]> };
const overview = (): MetroOverview => golden<MetroOverview>("views-overview-metro.json");
const runsOf = (view: OverviewResponse) => new Map<string, OverviewRun>(view.runs.map(run => [run.id, run]));

function mount(view: MetroOverview, overrides: Partial<MetroMapProps> = {}) {
  const props: MetroMapProps = {
    layout: view.metroLayout,
    runs: runsOf(view),
    focus: null,
    pinned: null,
    onHover: vi.fn(),
    onPin: vi.fn(),
    onOpenRun: vi.fn(),
    onOpenMachine: vi.fn(),
    ...overrides
  };
  render(<MetroMap {...props} />);
  return props;
}

afterEach(() => cleanup());

describe("Metro drawing", () => {
  it("names every belt pill and station, and draws each station glyph and interchange", () => {
    const view = overview();
    mount(view);
    for (const line of view.metroLayout.lines) expect(screen.getByRole("button", { name: line.name })).toBeTruthy();
    if (view.metroLayout.stations.length > 0) expect(document.querySelector(`.metro-station-name`)).not.toBeNull();
    const interchanges = view.metroLayout.stations.filter(station => station.glyph === "interchange").length;
    expect(document.querySelectorAll(".metro-interchange").length).toBe(interchanges);
  });

});

describe("Metro hover, pin and dimming", () => {
  it("dims lines, stations, dots and labels of unrelated belts while a belt is in focus", () => {
    const view = overview();
    mount(view, { focus: "issues" });
    const unrelated = view.metroLayout.lines.filter(line => line.beltId !== "issues");
    for (const line of unrelated) {
      const pill = screen.getByRole("button", { name: line.name });
      expect(pill.style.opacity).toBe("0.35");
    }
    const related = view.metroLayout.lines.find(line => line.beltId === "issues");
    if (related) expect(screen.getByRole("button", { name: related.name }).style.opacity).toBe("1");
  });

  it("dims a belt's route names with the belt, without marking any route taken", () => {
    const view = overview();
    mount(view, { focus: "issues" });
    const routes = view.metroLayout.lines.flatMap(line => line.routes.map(route => ({ beltId: line.beltId, route })));
    for (const { beltId, route } of routes) {
      const pill = document.querySelector<HTMLElement>(`[data-belt="${beltId}"][data-route="${route.id}"]`)!;
      expect(pill.style.opacity).toBe(beltId === "issues" ? "1" : "0.35");
      expect(pill.className).not.toContain("is-not-taken");
    }
    for (const station of view.metroLayout.stations) {
      const chip = document.querySelector<HTMLElement>(`[data-status="${station.id}"]`)!;
      expect(chip.style.opacity).toBe(station.belts.includes("issues") ? "1" : "0.35");
    }
  });

  it("reports a pill's hover and clicks to pin the belt", async () => {
    const user = userEvent.setup();
    const view = overview();
    const props = mount(view);
    const first = view.metroLayout.lines[0]!;
    const pill = screen.getByRole("button", { name: first.name });
    await user.hover(pill);
    expect(props.onHover).toHaveBeenLastCalledWith(first.beltId, true);
    await user.click(pill);
    expect(props.onPin).toHaveBeenCalledWith(first.beltId);
  });
});

describe("Metro run cards and navigation", () => {
  it("shows an individual run's subject, repository and location on hover", () => {
    const view = overview();
    mount(view);
    const dot = view.metroLayout.dots.find(item => item.overflow === 0 && item.runIds.length > 0);
    if (!dot) return;
    const run = view.runs.find(item => item.id === dot.runIds[0])!;
    fireEvent.pointerEnter(document.querySelector(`.metro-dot[aria-label*="${run.ref}"]`)!);
    const card = document.querySelector(".metro-card")!;
    expect(card.textContent).toContain(run.ref);
    expect(card.textContent).toContain(run.repository);
    expect(card.textContent).toContain(run.location.text);
  });

  it("opens the machine when an overflow dot is clicked", async () => {
    const user = userEvent.setup();
    const view = overview();
    const overflowDot = view.metroLayout.dots.find(item => item.overflow > 0);
    if (!overflowDot) return;
    const props = mount(view);
    await user.click(document.querySelector<HTMLButtonElement>(".metro-dot.is-overflow")!);
    expect(props.onOpenMachine).toHaveBeenCalledWith(overflowDot.stationId);
  });

  it("says how many more runs an overflow dot holds, and that clicking shows them", () => {
    const view = overview();
    const overflowDot = view.metroLayout.dots.find(item => item.overflow > 0);
    if (!overflowDot) return;
    mount(view);
    fireEvent.pointerEnter(document.querySelector(".metro-dot.is-overflow")!);
    expect(document.querySelector(".metro-card")!.textContent).toBe(`${overflowDot.overflow} more runs hereClick to see them`);
  });

  it("shows the card when a dot takes keyboard focus, and places it inside the map", () => {
    const view = overview();
    mount(view);
    const dot = view.metroLayout.dots[0]!;
    fireEvent.focus(document.querySelector(`.metro-dot[data-dot="${dot.id}"]`)!);
    const card = document.querySelector<HTMLElement>(".metro-card")!;
    expect(card.style.left).toBe(`${dot.card.left}px`);
    expect(card.style.top).toBe(`${dot.card.top}px`);
    fireEvent.blur(document.querySelector(`.metro-dot[data-dot="${dot.id}"]`)!);
    expect(document.querySelector(".metro-card")).toBeNull();
  });

  it("opens a station's Work by machine, and a grouped label's first run needing attention", async () => {
    const user = userEvent.setup();
    const view = overview();
    const props = mount(view);
    const station = view.metroLayout.stations[0]!;
    await user.click(document.querySelector(".metro-station-name")!);
    expect(props.onOpenMachine).toHaveBeenCalledWith(station.id);
    const label = view.metroLayout.labels.find(item => item.runIds.length > 1);
    if (label) {
      const expected = label.openRunId;
      const button = Array.from(document.querySelectorAll<HTMLButtonElement>(".metro-label")).find(item => item.textContent === label.text)!;
      await user.click(button);
      expect(props.onOpenRun).toHaveBeenCalledWith(expected, view.runs.find(run => run.id === expected)?.openStage);
    }
  });
});
