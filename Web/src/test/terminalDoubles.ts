import { act } from "react";
import { vi } from "vitest";
import { AppSocket } from "./setup";

/**
 * Stand-ins for the xterm Terminal and FitAddon interfaces the Terminal component uses, and a WebSocket a
 * test drives by hand. They record what the component asks of them, so tests assert on the protocol and on
 * ownership (what was opened, written, sent, closed and disposed) rather than on a rendered terminal.
 */

export class MockTerminal {
  static instances: MockTerminal[] = [];
  cols = 80;
  rows = 24;
  opened: HTMLElement | null = null;
  addons: unknown[] = [];
  written: Uint8Array[] = [];
  disposed = false;
  listenersDisposed = 0;
  private listeners = new Set<(data: string) => void>();

  constructor() {
    MockTerminal.instances.push(this);
  }

  open(parent: HTMLElement): void {
    this.opened = parent;
  }

  loadAddon(addon: unknown): void {
    this.addons.push(addon);
  }

  write(data: Uint8Array): void {
    this.written.push(data);
  }

  onData(listener: (data: string) => void): { dispose(): void } {
    this.listeners.add(listener);
    return {
      dispose: () => {
        this.listeners.delete(listener);
        this.listenersDisposed += 1;
      }
    };
  }

  /** What the user types or pastes, as xterm reports it. */
  type(data: string): void {
    for (const listener of [...this.listeners]) listener(data);
  }

  dispose(): void {
    this.disposed = true;
  }
}

export class MockFit {
  static instances: MockFit[] = [];
  fits = 0;
  disposed = false;

  constructor() {
    MockFit.instances.push(this);
  }

  fit(): void {
    this.fits += 1;
  }

  dispose(): void {
    this.disposed = true;
  }
}

/** Delivers socket events inside `act`, so the state a component sets from them renders before the test reads it. */
function dispatch(target: EventTarget, ...events: Event[]): void {
  act(() => { for (const event of events) target.dispatchEvent(event); });
}

export class FakeSocket extends EventTarget {
  static instances: FakeSocket[] = [];
  readyState = 0;
  binaryType = "blob";
  sent: Array<string | Uint8Array> = [];
  closed: Array<[number, string]> = [];
  readonly url: string;

  constructor(url: string) {
    super();
    this.url = url;
    FakeSocket.instances.push(this);
  }

  send(data: string | Uint8Array): void {
    this.sent.push(data);
  }

  close(code = 1000, reason = ""): void {
    this.closed.push([code, reason]);
    this.readyState = 3;
  }

  /** The daemon accepted the attachment. */
  open(): void {
    this.readyState = 1;
    dispatch(this, new Event("open"));
  }

  /** A text control frame, such as the daemon's `mode` permission. */
  receiveText(text: string): void {
    dispatch(this, new MessageEvent("message", { data: text }));
  }

  /** Binary terminal output, delivered as an ArrayBuffer as the browser does with `binaryType` set. */
  receiveBytes(bytes: Uint8Array): void {
    dispatch(this, new MessageEvent("message", { data: bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) }));
  }

  /** The connection failed: an error, then the close the browser always follows it with. */
  fail(): void {
    this.readyState = 3;
    dispatch(this, new Event("error"), new Event("close"));
  }

  /** The connection dropped without an error event. */
  drop(): void {
    this.readyState = 3;
    dispatch(this, new Event("close"));
  }

  /** The binary frames this socket has sent, decoded as UTF-8 text. */
  sentText(): string[] {
    return this.sent.filter(isBinaryFrame).map(data => new TextDecoder().decode(data));
  }
}

/**
 * Whether a sent frame is binary. Under jsdom, `TextEncoder` can return a `Uint8Array` from Node's realm rather
 * than the test window's, so `instanceof Uint8Array` is not a reliable test.
 */
export function isBinaryFrame(data: string | Uint8Array): data is Uint8Array {
  return typeof data !== "string" && ArrayBuffer.isView(data);
}

/**
 * Stubs `WebSocket` so terminal attachments get a `FakeSocket` a test drives by hand, while the app's event
 * connection keeps the setup's `AppSocket`, which says hello on its own and is not counted as a terminal.
 */
export function stubTerminalSockets(): void {
  vi.stubGlobal("WebSocket", function (url: string | URL) {
    return String(url).includes("/api/v1/terminals") ? new FakeSocket(String(url)) : new AppSocket(String(url));
  });
}

export function resetTerminalDoubles(): void {
  MockTerminal.instances = [];
  MockFit.instances = [];
  FakeSocket.instances = [];
}
