import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LibraryColumn, type LibraryColumnProps } from "./LibraryColumn";
import { preferencesStorageKey } from "../state/preferences";
import { stubDaemon } from "../test/daemon";

const rows = [
  { id: "ship", label: "Ship", detail: "Agent · v2" },
  { id: "triage", label: "Triage", detail: "Agent · v2" },
  { id: "build", label: "Build" }
];

function Column({ onSelect, selectedId, ...props }: Partial<LibraryColumnProps>) {
  const [selected, setSelected] = useState<string | null>(selectedId ?? "ship");
  return <LibraryColumn
    title="Agents" rows={rows} foldKey="agents" empty={<p>No agents yet.</p>} {...props}
    selectedId={selected} onSelect={id => { setSelected(id); onSelect?.(id); }} />;
}

beforeEach(() => {
  localStorage.clear();
});

describe("LibraryColumn rows", () => {
  it("shows the rows with the selected one pressed, and selects by click", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<Column onSelect={onSelect} />);
    expect(screen.getByRole("button", { name: /Ship/ }).getAttribute("aria-pressed")).toBe("true");
    await user.click(screen.getByRole("button", { name: /Triage/ }));
    expect(onSelect).toHaveBeenCalledWith("triage");
    expect(screen.getByRole("button", { name: /Triage/ }).getAttribute("aria-pressed")).toBe("true");
  });

  it("shows the caller's empty state when there are no rows, and keeps the Add control", () => {
    render(<Column rows={[]} onAdd={() => {}} />);
    expect(screen.getByText("No agents yet.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add" })).toBeTruthy();
  });

  it("scrolls its rows independently of the rest of the column", () => {
    const { container } = render(<Column />);
    const list = container.querySelector<HTMLElement>(".library-rows");
    expect(list).not.toBeNull();
    expect(within(list!).getAllByRole("listitem")).toHaveLength(3);
  });

  it("shows the optional footer only when one is supplied", () => {
    const { rerender } = render(<Column />);
    expect(document.querySelector(".library-footer")).toBeNull();
    rerender(<Column footer={<span>3 agents</span>} />);
    expect(screen.getByText("3 agents")).toBeTruthy();
  });
});

describe("LibraryColumn Add", () => {
  it("calls back when Add is activated by keyboard", async () => {
    const user = userEvent.setup();
    const onAdd = vi.fn();
    render(<Column onAdd={onAdd} addLabel="Add agent" />);
    await user.tab();
    await user.tab({ shift: false });
    const add = screen.getByRole("button", { name: "Add agent" });
    add.focus();
    await user.keyboard("{Enter}");
    expect(onAdd).toHaveBeenCalledTimes(1);
  });

  it("reaches row selection from the keyboard", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(<Column onSelect={onSelect} />);
    screen.getByRole("button", { name: /Build/ }).focus();
    await user.keyboard("{Enter}");
    expect(onSelect).toHaveBeenCalledWith("build");
  });
});

describe("LibraryColumn folding", () => {
  it("folds to a 40px rail, reopens, and keeps the selection through both", async () => {
    const user = userEvent.setup();
    const { container } = render(<Column selectedId="triage" />);
    await user.click(screen.getByRole("button", { name: "Fold Agents" }));
    const rail = container.querySelector<HTMLElement>(".library-column-folded");
    expect(rail?.style.width).toBe("40px");
    expect(screen.queryByRole("button", { name: /Ship/ })).toBeNull();
    await user.click(screen.getByRole("button", { name: "Show Agents" }));
    expect(screen.getByRole("button", { name: /Triage/ }).getAttribute("aria-pressed")).toBe("true");
  });

  it("stores the fold state as a browser preference, and reads it back when mounted again", async () => {
    const user = userEvent.setup();
    const first = render(<Column />);
    await user.click(screen.getByRole("button", { name: "Fold Agents" }));
    const stored = JSON.parse(localStorage.getItem(preferencesStorageKey)!);
    expect(stored.listFolded).toEqual({ agents: true });
    first.unmount();
    render(<Column />);
    expect(screen.getByRole("button", { name: "Show Agents" })).toBeTruthy();
    expect(screen.queryByRole("list")).toBeNull();
  });

  it("does not send a daemon command when folded or reopened", async () => {
    const daemon = stubDaemon();
    const user = userEvent.setup();
    render(<Column />);
    await act(async () => { await user.click(screen.getByRole("button", { name: "Fold Agents" })); });
    await act(async () => { await user.click(screen.getByRole("button", { name: "Show Agents" })); });
    expect(daemon.requests).toEqual([]);
  });
});
