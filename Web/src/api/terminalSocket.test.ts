import { describe, expect, it, vi } from "vitest";
import { FakeSocket } from "../test/terminalDoubles";
import { TerminalSocket, parseModeFrame, terminalSocketUrl } from "./terminalSocket";

describe("terminal socket URLs", () => {
  it("stays on the page's own origin, as ws for http and wss for https", () => {
    expect(terminalSocketUrl({ protocol: "http:", host: "127.0.0.1:7331" }, { session: "bl-one", takeOver: false }))
      .toBe("ws://127.0.0.1:7331/api/v1/terminals?session=bl-one");
    expect(terminalSocketUrl({ protocol: "https:", host: "beltline.example:443" }, { session: "bl-one", takeOver: false }))
      .toBe("wss://beltline.example:443/api/v1/terminals?session=bl-one");
  });

  it("encodes session names as one query value", () => {
    const url = new URL(terminalSocketUrl({ protocol: "http:", host: "127.0.0.1:7331" }, { session: "bl/run&x=1 y#z", takeOver: false }));
    expect(url.searchParams.get("session")).toBe("bl/run&x=1 y#z");
    expect(url.searchParams.has("x")).toBe(false);
    expect(url.search).toContain("session=bl%2Frun%26x%3D1%20y%23z");
  });

  it("asks for take-over only on an explicit target, never by default", () => {
    const watch = terminalSocketUrl({ protocol: "http:", host: "h:1" }, { session: "bl-one", takeOver: false });
    expect(watch).not.toContain("takeOver");
    const takeOver = terminalSocketUrl({ protocol: "http:", host: "h:1" }, { session: "bl-one", takeOver: true });
    expect(new URL(takeOver).searchParams.get("takeOver")).toBe("1");
  });

  it("carries no credentials or token in the URL", () => {
    const url = terminalSocketUrl({ protocol: "https:", host: "h:1" }, { session: "bl-one", takeOver: true });
    expect(url).not.toMatch(/token|auth|@|password/i);
  });
});

describe("terminal mode frames", () => {
  it("accepts only a mode frame with a boolean readOnly", () => {
    expect(parseModeFrame('{"type":"mode","readOnly":true}')).toEqual({ readOnly: true });
    expect(parseModeFrame('{"type":"mode","readOnly":false}')).toEqual({ readOnly: false });
    expect(parseModeFrame('{"type":"mode","readOnly":"false"}')).toBeNull();
    expect(parseModeFrame('{"type":"mode"}')).toBeNull();
    expect(parseModeFrame('{"type":"resize","cols":80,"rows":24}')).toBeNull();
    expect(parseModeFrame("not json")).toBeNull();
    expect(parseModeFrame("null")).toBeNull();
  });
});

describe("terminal socket adapter", () => {
  function adapter(overrides: Partial<ConstructorParameters<typeof TerminalSocket>[0]> = {}) {
    const sockets: FakeSocket[] = [];
    const output: Uint8Array[] = [];
    const writable: boolean[] = [];
    const statuses: string[] = [];
    const onOpen = vi.fn();
    const socket = new TerminalSocket({
      url: "ws://h:1/api/v1/terminals?session=bl-one",
      socketFactory: url => { const created = new FakeSocket(url); sockets.push(created); return created as unknown as WebSocket; },
      onOutput: bytes => output.push(bytes),
      onOpen,
      onStatus: status => statuses.push(status),
      onWritable: value => writable.push(value),
      ...overrides
    });
    return { socket, sockets, output, writable, statuses, onOpen };
  }

  it("requests binary frames and reports readiness once the daemon accepts the socket", () => {
    const { sockets, statuses, onOpen } = adapter();
    expect(sockets[0]!.binaryType).toBe("arraybuffer");
    sockets[0]!.open();
    expect(statuses).toEqual(["live"]);
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("sends input only after a writable mode, as UTF-8 binary, and resize as text at any time", () => {
    const { socket, sockets, writable } = adapter();
    sockets[0]!.open();
    socket.sendResize(90, 20);
    socket.sendInput("early");
    sockets[0]!.receiveText('{"type":"mode","readOnly":true}');
    socket.sendInput("watching");
    sockets[0]!.receiveText('{"type":"mode","readOnly":false}');
    socket.sendInput("héllo");
    expect(writable).toEqual([true]);
    expect(sockets[0]!.sent[0]).toBe('{"type":"resize","cols":90,"rows":20}');
    expect(sockets[0]!.sentText()).toEqual(["héllo"]);
  });

  it("delivers binary output to the output handler and ignores frames that are not permissions", () => {
    const { sockets, output } = adapter();
    sockets[0]!.open();
    sockets[0]!.receiveBytes(new Uint8Array([1, 2, 3]));
    sockets[0]!.receiveText("not a mode frame");
    expect(output.map(bytes => Array.from(bytes))).toEqual([[1, 2, 3]]);
  });

  it("withdraws input and reports offline on error or drop, and close removes its listeners and closes once", () => {
    const { socket, sockets, writable, statuses } = adapter();
    sockets[0]!.open();
    sockets[0]!.receiveText('{"type":"mode","readOnly":false}');
    sockets[0]!.fail();
    expect(writable).toEqual([true, false]);
    expect(statuses.at(-1)).toBe("offline");
    socket.sendInput("after");
    expect(sockets[0]!.sentText()).toEqual([]);

    socket.close();
    socket.close();
    expect(sockets[0]!.closed).toEqual([[1000, "terminal closed"]]);
  });

  it("keeps input off when a writable mode frame arrives after an error, even if the socket still reports open", () => {
    const { socket, sockets, writable, statuses, output } = adapter();
    sockets[0]!.open();
    sockets[0]!.receiveText('{"type":"mode","readOnly":false}');
    sockets[0]!.dispatchEvent(new Event("error"));
    sockets[0]!.receiveText('{"type":"mode","readOnly":false}');
    sockets[0]!.receiveBytes(new Uint8Array([7]));
    socket.sendInput("after");
    socket.sendResize(90, 20);
    expect(writable).toEqual([true, false]);
    expect(statuses.at(-1)).toBe("offline");
    expect(output).toEqual([]);
    expect(sockets[0]!.sent).toEqual([]);
  });

  it("keeps input off when a writable mode frame arrives after a drop without an error", () => {
    const { socket, sockets, writable, statuses } = adapter();
    sockets[0]!.open();
    sockets[0]!.receiveText('{"type":"mode","readOnly":false}');
    sockets[0]!.drop();
    sockets[0]!.receiveText('{"type":"mode","readOnly":false}');
    socket.sendInput("after");
    expect(writable).toEqual([true, false]);
    expect(statuses.at(-1)).toBe("offline");
    expect(sockets[0]!.sentText()).toEqual([]);
  });

  it("drops the permission when the attachment is closed by the client", () => {
    const { socket, sockets, writable } = adapter();
    sockets[0]!.open();
    sockets[0]!.receiveText('{"type":"mode","readOnly":false}');
    socket.close();
    expect(writable).toEqual([true, false]);
    sockets[0]!.receiveBytes(new Uint8Array([9]));
    expect(writable).toEqual([true, false]);
  });
});
