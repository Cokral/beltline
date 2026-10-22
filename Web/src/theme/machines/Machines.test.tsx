import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import "./machines.css";
import nativePathsFile from "./nativePaths.json";
import { machineComponents, machineFaces, machineIds, machineTints } from "./Machines";

const machineStyles = readFileSync(resolve(process.cwd(), "src/theme/machines/machines.css"), "utf8");
/** Each machine's static paths in draw order. */
const nativePaths = nativePathsFile as Record<string, string[]>;

describe.each(machineIds)("%s machine", machine => {
  const Machine = machineComponents[machine];

  it("renders all seven face states with the state-specific cues", () => {
    const { container, rerender } = render(<Machine face="idle" />);
    const drawing = container.querySelector("svg")!;
    for (const face of machineFaces) {
      rerender(<Machine face={face} />);
      expect(drawing.getAttribute("data-face")).toBe(face);
      expect(drawing.getAttribute("viewBox")).toBe("0 0 120 108");
      expect(drawing.querySelector(".machine-face")?.getAttribute("data-face")).toBe(face);
      if (face === "wait" || face === "held") expect(drawing.querySelector("text")?.textContent).toBe(face === "wait" ? "!" : "?");
      if (face === "happy") expect(drawing.querySelector(".machine-face path")).toBeTruthy();
      if (face === "paused" || face === "todo") {
        const eyes = drawing.querySelector(".machine-face path");
        expect(eyes?.getAttribute("d")).toContain("h8");
        expect(eyes?.getAttribute("stroke")).toBe("#CFE0F7");
      }
      if (face === "run") expect(drawing.querySelector(".machine-eyes--run")).toBeTruthy();
    }
    if (machine === "mailbox") {
      rerender(<Machine face="wait" />);
      expect(container.querySelector(".machine-mail-flag--down")).toBeNull();
      rerender(<Machine face="idle" />);
      expect(container.querySelector(".machine-mail-flag--down")).toBeTruthy();
    }
  });

  it("accepts each of the nine native tints", () => {
    const { container, rerender } = render(<Machine tint="blue" />);
    for (const tint of machineTints) {
      rerender(<Machine tint={tint} />);
      expect(container.querySelector("svg")?.getAttribute("data-tint")).toBe(tint);
      expect(container.querySelector("svg")?.getAttribute("style")).toContain(`--tint-${tint}`);
    }
  });

  it("scales into proportional and letterboxed dimensions and supports dimming", () => {
    const { container, rerender } = render(<Machine width={120} height={108} />);
    const drawing = container.querySelector("svg")!;
    for (const [width, height] of [[60, 54], [120, 108], [240, 216], [160, 108]]) {
      rerender(<Machine width={width} height={height} />);
      expect(drawing.getAttribute("width")).toBe(String(width));
      expect(drawing.getAttribute("height")).toBe(String(height));
      expect(drawing.getAttribute("preserveAspectRatio")).toBe("xMidYMid meet");
    }
    rerender(<Machine dimmed />);
    expect(drawing.classList.contains("machine-dimmed")).toBe(true);
    expect(machineStyles).toMatch(/\.machine-dimmed\s*\{\s*opacity:\s*0\.45;\s*\}/);
    rerender(<Machine dimmed={false} />);
    expect(drawing.classList.contains("machine-dimmed")).toBe(false);
  });

  it("freezes animations on request while keeping the state cue", () => {
    const { container, rerender } = render(<Machine face="run" />);
    const drawing = container.querySelector("svg")!;
    expect(drawing.querySelector(".machine-eyes--run")).toBeTruthy();
    rerender(<Machine face="run" frozen />);
    expect(drawing.classList.contains("machine-frozen")).toBe(true);
    expect(drawing.querySelector(".machine-eyes--run")).toBeTruthy();
    rerender(<Machine face="wait" paused />);
    expect(drawing.classList.contains("machine-paused")).toBe(true);
    expect(drawing.querySelector("text")?.textContent).toBe("!");
  });

  it("preserves the native static path geometry and draw order", () => {
    const { container } = render(<Machine face="idle" frozen />);
    const svg = container.querySelector("svg")!;
    const expected = nativePaths[machine];
    const actual = [...svg.querySelectorAll("path")]
      .filter(path => !path.closest(".machine-face"))
      .map(path => path.getAttribute("d"));
    expect(actual).toEqual(expected);
  });
});

describe("native drawing details", () => {
  it("keeps the mailbox dome arc and state-dependent flag layering", () => {
    const Mailbox = machineComponents.mailbox;
    const { container, rerender } = render(<Mailbox face="idle" frozen />);
    const svg = container.querySelector("svg")!;
    expect([...svg.querySelectorAll("path")].map(path => path.getAttribute("d"))).toContain("M32,93 V46 A28,28 0 0 1 88,46 V93 Z");
    expect(svg.querySelector(".machine-mail-flag--down")).toBeTruthy();
    rerender(<Mailbox face="wait" frozen />);
    expect(svg.querySelector(".machine-mail-flag--down")).toBeNull();
    expect(svg.querySelector(".machine-motion--bubble text")?.textContent).toBe("!");
  });

  it("draws the robot's dashed gear ring before its centre knob and body", () => {
    const Robot = machineComponents.robot;
    const { container } = render(<Robot face="idle" frozen />);
    const svg = container.querySelector("svg")!;
    const ring = svg.querySelector('circle[cx="60"][cy="74"][r="8"]')!;
    const knob = svg.querySelector('circle[cx="60"][cy="74"][r="4"]')!;
    const body = svg.querySelector('rect[x="34"][y="58"][width="52"][height="32"]')!;
    expect(ring.getAttribute("fill")).toBe("none");
    expect(ring.getAttribute("stroke-width")).toBe("4");
    expect(ring.getAttribute("stroke-dasharray")).toBe("2.6 2.6");
    expect(body.compareDocumentPosition(ring) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(ring.compareDocumentPosition(knob) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});

