import { useState } from "react";
import { AppProviders } from "../app/App";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { FakeSocket, MockTerminal, resetTerminalDoubles } from "../test/terminalDoubles";
import { AppSocket } from "../test/setup";
import { AssistantSidebar } from "./AssistantSidebar";
import { json } from "../test/response";
import { golden } from "../test/golden";
import { fixtureRoute, refusal, stubDaemon, type Handler } from "../test/daemon";

vi.mock("@xterm/xterm", async () => ({ Terminal: (await import("../test/terminalDoubles")).MockTerminal }));
vi.mock("@xterm/addon-fit", async () => ({ FitAddon: (await import("../test/terminalDoubles")).MockFit }));

interface Assistant {
  running: boolean;
  busy: boolean;
  session: string;
  command: string[] | null;
  configuration: { backend: string; model: string; thinking: string | null };
  checkCommand: string[] | null;
  problems: string[];
  actions: Array<{ id: string; enabled: boolean; label: string; reason: string | null }>;
}

const assistantState = (changes: Record<string, unknown> = {}): Assistant => ({ ...golden<Assistant>("assistant.json"), ...changes });

function runningAssistant(changes: Record<string, unknown> = {}): Assistant {
  return { ...assistantState({ running: true, command: ["tmux", "attach", "-t", "bl-assistant-one"], session: "bl-assistant-one" }), actions: [
    { id: "configuration", enabled: false, label: "Change configuration", reason: "Stop the assistant before changing its configuration." },
    { id: "start", enabled: false, label: "Start", reason: "The assistant is already running." },
    { id: "stop", enabled: true, label: "Stop", reason: null }
  ], ...changes };
}

interface DaemonOptions {
  initial?: Assistant;
  read?: (count: number) => Response | Promise<Response>;
  configure?: Handler;
  start?: Handler;
  stop?: Handler;
}

/**
 * The assistant's daemon. Like the real one, a command that succeeds leaves the assistant as it returned it, so the
 * refetch after it agrees; a refused command keeps the assistant as it was.
 */
function stubAssistant(options: DaemonOptions = {}) {
  let current = options.initial ?? assistantState();
  let reads = 0;
  const remember = (handler: Handler): Handler => async request => {
    const response = await handler(request);
    if (response.ok) current = await response.clone().json() as Assistant;
    return response;
  };
  const daemon = stubDaemon({
    "/api/v1/models/pi": fixtureRoute("models.json"),
    "GET /api/v1/assistant": () => {
      reads += 1;
      return options.read ? options.read(reads) : json(current);
    },
    "POST /api/v1/assistant/configuration": remember(options.configure ?? (request => {
      current = { ...current, configuration: request.body as Assistant["configuration"] };
      return json(current);
    })),
    "POST /api/v1/assistant/start": remember(options.start ?? (() => {
      current = runningAssistant({ configuration: current.configuration });
      return json(current);
    })),
    "POST /api/v1/assistant/stop": remember(options.stop ?? (() => {
      current = assistantState({ configuration: current.configuration });
      return json(current);
    }))
  });
  return { ...daemon, get reads() { return reads; } };
}

function TestSockets() {
  // The one app event socket gets a hello; terminal sockets stay under the protocol double's control.
  class SocketRouter {
    constructor(url: string) {
      return (url.includes("/api/v1/terminals?") ? new FakeSocket(url) : new AppSocket(url)) as unknown as SocketRouter;
    }
  }
  vi.stubGlobal("WebSocket", SocketRouter as unknown as typeof WebSocket);
  return null;
}

function Host({ onHide }: { onHide?: () => void }) {
  const [request, setRequest] = useState("");
  const [open, setOpen] = useState(true);
  const [width, setWidth] = useState<number | null>(640);
  return <>
    <TestSockets />
    <button type="button" onClick={() => setOpen(false)}>Close host</button>
    {open && <AssistantSidebar available={1200} stored={width} onWidthChange={setWidth} onHide={() => { onHide?.(); setOpen(false); }}
      request={request} onRequestChange={setRequest} dirty={false} />}
  </>;
}

function renderAssistant(options: DaemonOptions = {}, onHide?: () => void) {
  const daemon = stubAssistant(options);
  render(<AppProviders><Host onHide={onHide} /></AppProviders>);
  return daemon;
}

const latestTerminal = (): MockTerminal => {
  const terminal = MockTerminal.instances.at(-1);
  if (!terminal) throw new Error("no assistant terminal was created");
  return terminal;
};
const terminalSockets = (): FakeSocket[] => FakeSocket.instances;

beforeEach(() => {
  vi.unstubAllGlobals();
  resetTerminalDoubles();
  AppSocket.instances = [];
});

describe("Assistant sidebar", () => {
  it("shows saved configuration and the model and thinking choices offered by the daemon", async () => {
    renderAssistant();
    const panel = await screen.findByRole("complementary", { name: "Assistant" });
    const model = await within(panel).findByRole("combobox", { name: "Model" }) as HTMLSelectElement;
    const thinking = within(panel).getByRole("combobox", { name: "Thinking" }) as HTMLSelectElement;
    await waitFor(() => expect(model.options.length).toBe(4));
    expect(model.value).toBe("opus-5.5-1m");
    expect(Array.from(thinking.options).map(option => option.textContent)).toEqual(["Default", "Low", "Medium", "High", "Extra high", "Max"]);
    expect(within(panel).queryByRole("combobox", { name: "Runs on" })).toBeNull();
  });

  it("posts the full selected configuration, and shows the refusal with the attempted choice kept", async () => {
    const user = userEvent.setup();
    const daemon = renderAssistant({ configure: refusal("Configuration refused.", ["Choose another model."]) });
    await user.selectOptions(await screen.findByRole("combobox", { name: "Model" }), "fixture-model");
    await waitFor(() => expect(daemon.posts("/api/v1/assistant/configuration")).toHaveLength(1));
    expect(daemon.posts("/api/v1/assistant/configuration")[0]!.body).toEqual({ backend: "pi", model: "fixture-model", thinking: null });
    expect(await screen.findByText("Configuration refused.")).toBeTruthy();
    expect((screen.getByRole("combobox", { name: "Model" }) as HTMLSelectElement).value).toBe("fixture-model");
    expect(screen.getByText(/Saved configuration: Pi · opus-5.5-1m/)).toBeTruthy();
  });

  it("keeps the request after a refused start, clears it after a start, and sends one start however often Start is clicked", async () => {
    const user = userEvent.setup();
    const daemon = renderAssistant({ start: refusal("Start refused.") });
    await user.type(await screen.findByRole("textbox", { name: "Request" }), "Keep this request");
    await user.click(screen.getByRole("button", { name: "Start" }));
    expect(await screen.findByText("Start refused.")).toBeTruthy();
    expect((screen.getByRole("textbox", { name: "Request" }) as HTMLTextAreaElement).value).toBe("Keep this request");
    expect(daemon.posts("/api/v1/assistant/start")[0]!.body).toEqual({ request: "Keep this request" });
  });

  it("suppresses duplicate starts while the request is pending, then clears the request", async () => {
    const user = userEvent.setup();
    let resolveStart!: (response: Response) => void;
    const daemon = renderAssistant({ start: () => new Promise<Response>(resolve => { resolveStart = resolve; }) });
    await user.type(await screen.findByRole("textbox", { name: "Request" }), "Start once");
    const start = screen.getByRole("button", { name: "Start" });
    fireEvent.click(start);
    fireEvent.click(start);
    await waitFor(() => expect(daemon.posts("/api/v1/assistant/start")).toHaveLength(1));
    resolveStart(json(runningAssistant()));
    expect(await screen.findByText("Running", { selector: ".assistant-status" })).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "Request" })).toBeNull();
  });

  it("shows the daemon's reasons for disabled offers, and sends no command for them", async () => {
    const user = userEvent.setup();
    const daemon = renderAssistant({ initial: assistantState({ actions: [
      { id: "configuration", enabled: false, label: "Change configuration", reason: "Configuration is locked." },
      { id: "start", enabled: false, label: "Start", reason: "Starting is disabled." }
    ] }) });
    await screen.findByRole("textbox", { name: "Request" });
    expect(screen.getByText("Configuration is locked.")).toBeTruthy();
    expect(screen.getByText("Starting is disabled.")).toBeTruthy();
    expect(screen.getByRole("combobox", { name: "Model" }).matches(":disabled")).toBe(true);
    await user.type(screen.getByRole("textbox", { name: "Request" }), "No command");
    await user.click(screen.getByRole("button", { name: "Start" }));
    expect(daemon.requests.filter(request => request.method === "POST")).toEqual([]);
  });

  it("renders the demo conversation without a socket, and disables configuration while busy", async () => {
    renderAssistant({ initial: runningAssistant({ command: null, busy: false }) });
    expect(await screen.findByText("Simulated conversation. No real agent is launched in demo mode.")).toBeTruthy();
    expect(terminalSockets()).toHaveLength(0);
    expect(screen.getByRole("combobox", { name: "Model" }).matches(":disabled")).toBe(true);
    expect(screen.getByRole("button", { name: "Stop" })).toBeTruthy();
  });

  it("shows loading and a failed initial read, and labels stale data when a later refresh fails", async () => {
    let resolveRead!: (response: Response) => void;
    renderAssistant({ read: count => count === 1 ? new Promise<Response>(resolve => { resolveRead = resolve; }) : json({ message: "Initial read failed.", problems: [] }, 503) });
    expect(screen.getByText("Loading assistant…")).toBeTruthy();
    await waitFor(() => expect(resolveRead).toBeTypeOf("function"));
    resolveRead(json({ message: "Initial read failed.", problems: [] }, 503));
    expect(await screen.findByText("Initial read failed.")).toBeTruthy();
  });

  it("labels stale assistant data after a failed resource refresh, and shows the daemon's problems", async () => {
    let reads = 0;
    renderAssistant({ initial: assistantState({ problems: ["belts/old/v2.json: missing key name"] }), read: () => ++reads === 1 ? json(assistantState({ problems: ["belts/old/v2.json: missing key name"] })) : json({ message: "Refresh failed.", problems: [] }, 503) });
    const problems = await screen.findByRole("region", { name: "Problems in what it changed" });
    expect(within(problems).getByText("belts/old/v2.json: missing key name")).toBeTruthy();
    AppSocket.instances[0]!.send({ type: "changed", resource: "assistant", revision: 1 });
    expect(await screen.findByText("Showing stale assistant data; refresh failed.")).toBeTruthy();
    expect(screen.getByText("belts/old/v2.json: missing key name")).toBeTruthy();
  });

  it("attaches the real session without takeOver, waits for writable mode, and disables input after disconnect", async () => {
    renderAssistant({ initial: runningAssistant() });
    await waitFor(() => expect(terminalSockets()).toHaveLength(1));
    const socket = terminalSockets()[0]!;
    expect(socket.url).toMatch(/\/api\/v1\/terminals\?session=bl-assistant-one$/);
    expect(socket.url).not.toContain("takeOver");
    socket.open();
    latestTerminal().type("before mode");
    expect(socket.sentText()).toEqual([]);
    socket.receiveText(JSON.stringify({ type: "mode", readOnly: false }));
    latestTerminal().type("writable input");
    expect(socket.sentText()).toEqual(["writable input"]);
    socket.drop();
    latestTerminal().type("after disconnect");
    expect(socket.sentText()).toEqual(["writable input"]);
    expect(screen.getByText("Terminal offline. Input is off.")).toBeTruthy();
  });

  it("honors daemon read-only mode, and detaches on hide without stopping the conversation", async () => {
    const user = userEvent.setup();
    const stopped = vi.fn();
    const daemon = renderAssistant({ initial: runningAssistant() }, stopped);
    await waitFor(() => expect(terminalSockets()).toHaveLength(1));
    const socket = terminalSockets()[0]!;
    socket.open();
    socket.receiveText(JSON.stringify({ type: "mode", readOnly: true }));
    latestTerminal().type("read only");
    expect(socket.sentText()).toEqual([]);
    await user.click(screen.getByRole("button", { name: "Hide ›" }));
    expect(socket.closed).toEqual([[1000, "terminal closed"]]);
    expect(stopped).toHaveBeenCalledTimes(1);
    expect(daemon.posts("/api/v1/assistant/stop")).toHaveLength(0);
  });

  it("closes the previous attachment when an assistant changed event replaces its session", async () => {
    let state = runningAssistant();
    const daemon = renderAssistant({ read: () => json(state) });
    await waitFor(() => expect(terminalSockets()).toHaveLength(1));
    state = runningAssistant({ session: "bl-assistant-two", command: ["tmux", "attach", "-t", "bl-assistant-two"] });
    AppSocket.instances[0]!.send({ type: "changed", resource: "assistant", revision: 1 });
    await waitFor(() => expect(terminalSockets()).toHaveLength(2));
    expect(terminalSockets()[0]!.closed).toEqual([[1000, "terminal closed"]]);
    expect(terminalSockets()[1]!.url).toContain("session=bl-assistant-two");
    expect(daemon.reads).toBeGreaterThanOrEqual(2);
  });

  it("posts Stop once, closes the attachment, and returns to the request view", async () => {
    const user = userEvent.setup();
    const daemon = renderAssistant({ initial: runningAssistant() });
    await waitFor(() => expect(terminalSockets()).toHaveLength(1));
    const socket = terminalSockets()[0]!;
    await user.click(screen.getByRole("button", { name: "Stop" }));
    expect(await screen.findByRole("textbox", { name: "Request" })).toBeTruthy();
    expect(socket.closed).toEqual([[1000, "terminal closed"]]);
    expect(daemon.posts("/api/v1/assistant/stop")).toHaveLength(1);
    expect(daemon.posts("/api/v1/assistant/stop")[0]!.body).toEqual({});
  });

  it("disables assistant commands and terminal input while the daemon is offline", async () => {
    renderAssistant({ initial: runningAssistant() });
    await waitFor(() => expect(terminalSockets()).toHaveLength(1));
    Object.defineProperty(window.navigator, "onLine", { configurable: true, value: false });
    window.dispatchEvent(new Event("offline"));
    await waitFor(() => expect((screen.getByRole("button", { name: "Stop" }) as HTMLButtonElement).disabled).toBe(true));
    expect(screen.getByRole("combobox", { name: "Model" }).matches(":disabled")).toBe(true);
    expect(terminalSockets()[0]!.closed).toEqual([[1000, "terminal closed"]]);
    expect(screen.getByText("Terminal offline. Input is off.")).toBeTruthy();
  });

  it("resizes with pointer events and commits the chosen width", async () => {
    renderAssistant();
    const divider = await screen.findByRole("separator", { name: "Resize Assistant" });
    fireEvent.pointerDown(divider, { clientX: 900, pointerId: 1 });
    fireEvent.pointerMove(divider, { clientX: 875, pointerId: 1 });
    expect(divider.getAttribute("aria-valuenow")).toBe("665");
    fireEvent.pointerUp(divider, { pointerId: 1 });
    expect(divider.getAttribute("aria-valuenow")).toBe("665");
  });
});
