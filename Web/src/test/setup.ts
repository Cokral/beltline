import { cleanup, configure } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// The app loads this sheet in main.tsx; tests read computed layout from it.
import "../components/components.css";

// findBy and waitFor give up after 1 s by default, which a busy machine (several agents testing at once) overruns.
configure({ asyncUtilTimeout: 5000 });

export class AppSocket extends EventTarget {
  static instances: AppSocket[] = [];
  readyState = 0;
  readonly url: string;

  constructor(url: string) {
    super();
    this.url = url;
    AppSocket.instances.push(this);
    queueMicrotask(() => {
      if (this.readyState !== 0) return;
      this.readyState = 1;
      this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify({ type: "hello", apiVersion: 1 }) }));
    });
  }

  close(): void {
    this.readyState = 3;
    this.dispatchEvent(new Event("close"));
  }

  send(message: unknown): void {
    this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(message) }));
  }
}

/**
 * jsdom has no PointerEvent, so `fireEvent.pointerMove(…, { clientX })` would build a plain Event and drop the
 * coordinates. A MouseEvent carries them; the pointer fields the app reads are added on top.
 */
if (typeof window !== "undefined" && typeof window.PointerEvent === "undefined") {
  class TestPointerEvent extends MouseEvent {
    readonly pointerId: number;
    readonly pointerType: string;
    readonly isPrimary: boolean;
    constructor(type: string, init: PointerEventInit = {}) {
      super(type, init);
      this.pointerId = init.pointerId ?? 0;
      this.pointerType = init.pointerType ?? "mouse";
      this.isPrimary = init.isPrimary ?? true;
    }
  }
  window.PointerEvent = TestPointerEvent as unknown as typeof PointerEvent;
}

if (typeof window !== "undefined") {
  vi.stubGlobal("WebSocket", AppSocket);
  Object.defineProperty(window.navigator, "onLine", { configurable: true, value: true });
}

afterEach(() => {
  if (typeof window !== "undefined") cleanup();
  AppSocket.instances = [];
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  if (typeof window !== "undefined") {
    vi.stubGlobal("WebSocket", AppSocket);
    Object.defineProperty(window.navigator, "onLine", { configurable: true, value: true });
  }
});
