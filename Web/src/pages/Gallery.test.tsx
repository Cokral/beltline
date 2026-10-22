import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import "../theme/tokens.css";
import "../theme/fields.css";
import "../theme/machines/machines.css";
import Gallery from "./Gallery";
import { machineFaces, machineIds } from "../theme/machines/Machines";

describe("development theme gallery", () => {
  it("shows all tokens, fonts, controls, and all 91 labelled machine states without a daemon", () => {
    const { container } = render(<Gallery />);
    expect(screen.getByRole("heading", { name: "Theme gallery" })).toBeTruthy();
    expect(container.querySelectorAll(".gallery-token")).toHaveLength(125);
    expect(container.querySelectorAll(".gallery-font-sample")).toHaveLength(3);
    expect(container.querySelectorAll(".gallery-field-card")).toHaveLength(4);
    expect(container.querySelectorAll(".gallery-machine-grid--native svg")).toHaveLength(13);
    const stateSamples = container.querySelectorAll("#machines .gallery-machine-grid:not(.gallery-machine-grid--native) svg");
    expect(stateSamples).toHaveLength(91);
    expect([...stateSamples].map(svg => `${svg.getAttribute("data-machine")}:${svg.getAttribute("data-face")}`)).toEqual(machineIds.flatMap(machine => machineFaces.map(face => `${machine}:${face}`)));
    expect(screen.getByText("Native comparison · idle · frozen · 120 × 108")).toBeTruthy();
  });

  it("applies tint, size, dimming and frozen controls to every state sample", async () => {
    const user = userEvent.setup();
    const { container } = render(<Gallery />);
    const stateGrid = container.querySelector("#machines .gallery-machine-grid:not(.gallery-machine-grid--native)")!;
    const firstDrawing = stateGrid.querySelector("svg")!;
    expect(firstDrawing.getAttribute("data-tint")).toBe("blue");
    expect(firstDrawing.getAttribute("width")).toBe("120");
    expect(firstDrawing.classList.contains("machine-frozen")).toBe(true);

    await user.selectOptions(screen.getByRole("combobox", { name: "Machine tint" }), "coral");
    await user.selectOptions(screen.getByRole("combobox", { name: "Machine size" }), "3");
    await user.click(screen.getByRole("checkbox", { name: "Dimmed" }));
    await user.click(screen.getByRole("checkbox", { name: "Freeze animation" }));
    expect([...stateGrid.querySelectorAll("svg")].every(svg => svg.getAttribute("data-tint") === "coral")).toBe(true);
    expect(firstDrawing.getAttribute("width")).toBe("160");
    expect(firstDrawing.getAttribute("height")).toBe("108");
    expect(firstDrawing.classList.contains("machine-dimmed")).toBe(true);
    expect(firstDrawing.classList.contains("machine-frozen")).toBe(false);
    expect(container.querySelector(".gallery-state-heading")?.textContent).toContain("letterboxed");
  });

  it("keeps read-only and disabled samples immutable and allows keyboard segment selection", async () => {
    const user = userEvent.setup();
    render(<Gallery />);
    const readOnly = screen.getByRole("textbox", { name: "Read-only paper field" }) as HTMLInputElement;
    const disabled = screen.getByRole("textbox", { name: "Disabled paper field" }) as HTMLInputElement;
    expect(readOnly.readOnly).toBe(true);
    expect(disabled.disabled).toBe(true);
    await user.click(readOnly);
    await user.keyboard(" changed");
    expect(readOnly.value).toBe("Read-only sample");
    const readOnlySelect = screen.getByRole("combobox", { name: "Read-only select sample" }) as HTMLSelectElement;
    expect(readOnlySelect.disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Disabled · 40%" }) as HTMLButtonElement).disabled).toBe(true);

    const group = screen.getByRole("group", { name: "Segmented selection" });
    const web = within(group).getByRole("button", { name: "Web" });
    await user.tab();
    await user.click(web);
    expect(web.getAttribute("aria-pressed")).toBe("true");
    expect(within(group).getByRole("button", { name: "Native" }).getAttribute("aria-pressed")).toBe("false");
  });
});
