import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Terminal } from "./Terminal";
import { FakeSocket, isBinaryFrame, MockFit, MockTerminal, resetTerminalDoubles } from "../test/terminalDoubles";

vi.mock("@xterm/xterm", async () => ({ Terminal: (await import("../test/terminalDoubles")).MockTerminal }));
vi.mock("@xterm/addon-fit", async () => ({ FitAddon: (await import("../test/terminalDoubles")).MockFit }));

const target = { session: "bl-run-attempt", takeOver: false };
let observers: Array<{ callback: () => void; disconnected: boolean }> = [];

class FakeResizeObserver {
  private readonly record: { callback: () => void; disconnected: boolean };
  constructor(callback: () => void) {
    this.record = { callback, disconnected: false };
    observers.push(this.record);
  }
  observe(): void {}
  disconnect(): void { this.record.disconnected = true; }
}

/** The terminal of the current attachment: every new attachment creates its own, and the old one is disposed. */
function terminal(): MockTerminal {
  const current = MockTerminal.instances[MockTerminal.instances.length - 1];
  if (!current) throw new Error("no terminal was created");
  return current;
}

function socket(index = 0): FakeSocket {
  const found = FakeSocket.instances[index];
  if (!found) throw new Error(`socket ${index} was not opened`);
  return found;
}

beforeEach(() => {
  resetTerminalDoubles();
  observers = [];
  vi.stubGlobal("WebSocket", FakeSocket);
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
});

describe("terminal attachment", () => {
  it("hands binary output to terminal.write unchanged as a Uint8Array", () => {
    render(<Terminal target={target} />);
    socket().open();
    const bytes = new Uint8Array([27, 91, 72, 0xe2, 0x9c, 0x93]);
    socket().receiveBytes(bytes);
    const [written] = terminal().written;
    expect(written).toBeInstanceOf(Uint8Array);
    expect(Array.from(written!)).toEqual(Array.from(bytes));
  });

  it("fits to the known size and sends resize on socket readiness and on container resize", () => {
    render(<Terminal target={target} />);
    terminal().cols = 100;
    terminal().rows = 30;
    expect(MockFit.instances[0]!.fits).toBeGreaterThanOrEqual(1);
    expect(socket().sent).toEqual([]);
    socket().open();
    expect(socket().sent).toEqual([JSON.stringify({ type: "resize", cols: 100, rows: 30 })]);

    terminal().cols = 120;
    terminal().rows = 40;
    const fitsBefore = MockFit.instances[0]!.fits;
    observers[0]!.callback();
    expect(MockFit.instances[0]!.fits).toBe(fitsBefore + 1);
    expect(socket().sent.at(-1)).toBe(JSON.stringify({ type: "resize", cols: 120, rows: 40 }));
  });

  it("sends no input before a valid mode frame, and none while the attachment is read-only", () => {
    render(<Terminal target={target} />);
    socket().open();
    terminal().type("before");
    expect(socket().sent).toEqual([JSON.stringify({ type: "resize", cols: 80, rows: 24 })]);

    socket().receiveText("not a mode frame");
    socket().receiveText(JSON.stringify({ type: "mode", readOnly: "false" }));
    terminal().type("still before");
    expect(socket().sentText()).toEqual([]);

    socket().receiveText(JSON.stringify({ type: "mode", readOnly: true }));
    terminal().type("read only");
    expect(socket().sentText()).toEqual([]);
    expect(screen.getByText("Watching · read only")).toBeTruthy();
  });

  it("encodes writable keystrokes and paste as binary frames", () => {
    render(<Terminal target={target} />);
    socket().open();
    socket().receiveText(JSON.stringify({ type: "mode", readOnly: false }));
    terminal().type("ls\n");
    terminal().type("pasted text");
    expect(socket().sent.filter(data => !isBinaryFrame(data)).map(data => JSON.parse(data as string).type)).toEqual(["resize"]);
    expect(socket().sentText()).toEqual(["ls\n", "pasted text"]);
    expect(screen.getByText("Connected · input on")).toBeTruthy();
  });

  it("sends no input after the connection drops, even though the terminal still has focus", () => {
    render(<Terminal target={target} />);
    socket().open();
    socket().receiveText(JSON.stringify({ type: "mode", readOnly: false }));
    socket().drop();
    terminal().type("late");
    expect(socket().sentText()).toEqual([]);
    expect(screen.getByText("Terminal offline. Input is off.")).toBeTruthy();
  });

  it("shows an offline state, and Retry starts a fresh attachment with input off until its own mode frame", () => {
    render(<Terminal target={target} />);
    socket().open();
    socket().receiveText(JSON.stringify({ type: "mode", readOnly: false }));
    socket().fail();
    expect(screen.getByRole("status").textContent).toBe("Terminal offline. Input is off.");

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(FakeSocket.instances).toHaveLength(2);
    socket(1).open();
    terminal().type("typed before permission");
    expect(socket(1).sentText()).toEqual([]);
    socket(1).receiveText(JSON.stringify({ type: "mode", readOnly: false }));
    terminal().type("after permission");
    expect(socket(1).sentText()).toEqual(["after permission"]);
  });

  it("opens no socket while the daemon is offline, and disables Retry", () => {
    const { rerender } = render(<Terminal target={target} offline />);
    expect(FakeSocket.instances).toHaveLength(0);
    expect((screen.getByRole("button", { name: "Retry" }) as HTMLButtonElement).disabled).toBe(true);
    rerender(<Terminal target={target} />);
    expect(FakeSocket.instances).toHaveLength(1);
  });

  it("closes the old attachment when the target changes, and asks for take-over only in the new target's URL", () => {
    const { rerender } = render(<Terminal target={target} />);
    socket().open();
    rerender(<Terminal target={{ session: target.session, takeOver: true }} />);
    expect(socket(0).closed).toEqual([[1000, "terminal closed"]]);
    expect(socket(1).url).toBe(`ws://${window.location.host}/api/v1/terminals?session=bl-run-attempt&takeOver=1`);
    socket(1).open();
    socket(1).receiveText(JSON.stringify({ type: "mode", readOnly: false }));
    terminal().type("typed");
    expect(socket(1).sentText()).toEqual(["typed"]);
    expect(socket(0).sent.filter(isBinaryFrame)).toEqual([]);
  });

  it("disposes the terminal, fit add-on, observer and input listener on unmount, and closes the socket", () => {
    const { unmount } = render(<Terminal target={target} />);
    socket().open();
    unmount();
    expect(socket().closed).toEqual([[1000, "terminal closed"]]);
    expect(terminal().disposed).toBe(true);
    expect(MockFit.instances[0]!.disposed).toBe(true);
    expect(observers[0]!.disconnected).toBe(true);
    expect(terminal().listenersDisposed).toBe(1);
  });
});
