/**
 * The terminal attachment's wire protocol, as the browser sees it. Output arrives as binary frames; input
 * leaves as binary frames, and only while the daemon's first text `mode` frame says the attachment is
 * writable. Resize is a text frame that any attachment may send, watching or not.
 */

/** One attachment to a daemon terminal. `takeOver` is set only after the user activates an enabled offer. */
export interface TerminalTarget {
  session: string;
  takeOver: boolean;
}

export type TerminalStatus = "connecting" | "live" | "offline";

/** `WebSocket.OPEN`, spelled out so a test double needs only its readyState. */
const openReadyState = 1;

/** The same origin as the page, over `ws:` or `wss:` to match it. Credentials travel as the page's cookie, never in the URL. */
export function terminalSocketUrl(browser: Pick<Location, "protocol" | "host">, target: TerminalTarget): string {
  const scheme = browser.protocol === "https:" ? "wss:" : "ws:";
  const takeOver = target.takeOver ? "&takeOver=1" : "";
  return `${scheme}//${browser.host}/api/v1/terminals?session=${encodeURIComponent(target.session)}${takeOver}`;
}

/** The daemon's permission frame: `{"type":"mode","readOnly":true|false}`. Anything else is not a permission. */
export function parseModeFrame(text: string): { readOnly: boolean } | null {
  let decoded: unknown;
  try { decoded = JSON.parse(text) as unknown; }
  catch { return null; }
  if (!decoded || typeof decoded !== "object") return null;
  const fields = decoded as Record<string, unknown>;
  if (fields.type !== "mode" || typeof fields.readOnly !== "boolean") return null;
  return { readOnly: fields.readOnly };
}

export interface TerminalSocketOptions {
  url: string;
  socketFactory?: (url: string) => WebSocket;
  onOutput: (bytes: Uint8Array) => void;
  onOpen: () => void;
  onStatus: (status: TerminalStatus) => void;
  onWritable: (writable: boolean) => void;
}

/**
 * One attachment's socket. A new attachment starts with input off; a close or an error ends the attachment
 * for good, so later frames cannot restore input, and a `mode` frame that is not valid withholds input.
 * `close()` removes every listener this object added.
 */
export class TerminalSocket {
  private readonly socket: WebSocket;
  private writable = false;
  private closed = false;
  private failed = false;

  constructor(private readonly options: TerminalSocketOptions) {
    this.socket = (options.socketFactory ?? (url => new WebSocket(url)))(options.url);
    this.socket.binaryType = "arraybuffer";
    this.socket.addEventListener("open", this.handleOpen);
    this.socket.addEventListener("message", this.handleMessage);
    this.socket.addEventListener("close", this.handleClose);
    this.socket.addEventListener("error", this.handleError);
  }

  /** Keystrokes and paste, encoded as UTF-8 binary, only when the daemon reported this attachment writable. */
  sendInput(data: string): void {
    if (!this.writable || !this.isOpen()) return;
    this.socket.send(new TextEncoder().encode(data));
  }

  sendResize(columns: number, rows: number): void {
    if (!this.isOpen()) return;
    this.socket.send(JSON.stringify({ type: "resize", cols: columns, rows }));
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.setWritable(false);
    this.socket.removeEventListener("open", this.handleOpen);
    this.socket.removeEventListener("message", this.handleMessage);
    this.socket.removeEventListener("close", this.handleClose);
    this.socket.removeEventListener("error", this.handleError);
    this.socket.close(1000, "terminal closed");
  }

  private isOpen(): boolean {
    return !this.closed && !this.failed && this.socket.readyState === openReadyState;
  }

  private fail(): void {
    this.failed = true;
    this.setWritable(false);
    this.options.onStatus("offline");
  }

  private setWritable(writable: boolean): void {
    if (this.writable === writable) return;
    this.writable = writable;
    this.options.onWritable(writable);
  }

  private readonly handleOpen = (): void => {
    if (this.closed || this.failed) return;
    this.options.onStatus("live");
    this.options.onOpen();
  };

  private readonly handleMessage = (event: MessageEvent): void => {
    if (this.closed || this.failed) return;
    if (typeof event.data === "string") {
      const mode = parseModeFrame(event.data);
      if (mode) this.setWritable(!mode.readOnly);
      return;
    }
    // Not `instanceof ArrayBuffer`: a buffer from another realm (an iframe, or Node's under jsdom) fails that test.
    if (Object.prototype.toString.call(event.data) === "[object ArrayBuffer]") this.options.onOutput(new Uint8Array(event.data as ArrayBuffer));
  };

  private readonly handleClose = (): void => {
    if (this.closed || this.failed) return;
    this.fail();
  };

  private readonly handleError = (): void => {
    if (this.closed || this.failed) return;
    this.fail();
  };
}
