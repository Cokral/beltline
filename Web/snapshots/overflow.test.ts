import { afterEach, describe, expect, it } from "vitest";
import { findColumnOverflows } from "./overflow";

afterEach(() => { document.body.innerHTML = ""; });

describe("column overflow detector", () => {
  it("ignores fitting columns and reports an overflowing descendant", () => {
    Object.defineProperty(Range.prototype, "getClientRects", { configurable: true, value: () => [] });
    document.body.innerHTML = '<main data-column="fixture" style="width:100px"><span id="overflow-fixture" style="display:block;width:120px">x</span></main>';
    const element = document.querySelector("main")!;
    element.getBoundingClientRect = () => ({ left: 0, right: 100, top: 0, bottom: 100, width: 100, height: 100, x: 0, y: 0, toJSON: () => ({}) });
    const child = document.querySelector("span")!;
    child.getBoundingClientRect = () => ({ left: 0, right: 120, top: 0, bottom: 10, width: 120, height: 10, x: 0, y: 0, toJSON: () => ({}) });
    expect(findColumnOverflows()).toEqual([{ column: "fixture", element: "span#overflow-fixture", excess: 20 }]);
  });

  it("measures ellipsised text only up to the element that clips it", () => {
    const rect = (right: number) => ({ left: 0, right, top: 0, bottom: 10, width: right, height: 10, x: 0, y: 0, toJSON: () => ({}) });
    Object.defineProperty(Range.prototype, "getClientRects", { configurable: true, value: () => [rect(160)] });
    document.body.innerHTML = '<main data-column="nav" style="width:100px"><span id="name" style="display:block;overflow:hidden;text-overflow:ellipsis">FACTORY · A LONG NAME</span></main>';
    document.querySelector<HTMLElement>("main")!.getBoundingClientRect = () => rect(100);
    const name = document.querySelector<HTMLElement>("#name")!;
    name.getBoundingClientRect = () => rect(100);
    expect(findColumnOverflows()).toEqual([]);
    name.style.textOverflow = "clip";
    expect(findColumnOverflows()).toEqual([{ column: "nav", element: "span#name", excess: 60 }]);
  });

  it("ignores screen-reader-only text, which is clipped to nothing", () => {
    const rect = (right: number) => ({ left: 0, right, top: 0, bottom: 10, width: right, height: 10, x: 0, y: 0, toJSON: () => ({}) });
    Object.defineProperty(Range.prototype, "getClientRects", { configurable: true, value: () => [rect(400)] });
    document.body.innerHTML = '<main data-column="editor" style="width:100px"><span id="hidden" class="screen-reader-only" style="position:absolute;clip:rect(0 0 0 0);white-space:nowrap">A reason only screen readers hear</span></main>';
    document.querySelector<HTMLElement>("main")!.getBoundingClientRect = () => rect(100);
    document.querySelector<HTMLElement>("#hidden")!.getBoundingClientRect = () => rect(400);
    expect(findColumnOverflows()).toEqual([]);
  });

  it("allows map content that scrolls inside its viewport and still reports content escaping the viewport", () => {
    Object.defineProperty(Range.prototype, "getClientRects", { configurable: true, value: () => [] });
    document.body.innerHTML = '<main data-column="map" style="width:300px"><div id="viewport" data-scroll-viewport style="width:200px"><span id="wide" style="display:block;width:400px">map</span></div></main>';
    const rect = (right: number) => ({ left: 0, right, top: 0, bottom: 100, width: right, height: 100, x: 0, y: 0, toJSON: () => ({}) });
    document.querySelector<HTMLElement>("main")!.getBoundingClientRect = () => rect(300);
    const viewport = document.querySelector<HTMLElement>("#viewport")!;
    viewport.getBoundingClientRect = () => rect(200);
    Object.defineProperty(viewport, "scrollWidth", { configurable: true, value: 400 });
    const wide = document.querySelector<HTMLElement>("#wide")!;
    wide.getBoundingClientRect = () => rect(400);
    expect(findColumnOverflows()).toEqual([]);
    wide.getBoundingClientRect = () => rect(450);
    expect(findColumnOverflows()).toEqual([{ column: "map", element: "span#wide", excess: 50 }]);
  });
});
