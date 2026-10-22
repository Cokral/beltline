import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { EventConnectionManager } from "./eventConnection";

class FakeSocket extends EventTarget {
  readyState = 0;
  closeCalls: Array<[number, string]> = [];
  close(code = 1000, reason = ""): void {
    this.closeCalls.push([code, reason]);
    this.readyState = 3;
    this.dispatchEvent(new Event("close"));
  }
  send(value: unknown): void {
    this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(value) }));
  }
  fail(): void { this.dispatchEvent(new Event("error")); }
}

function makeQueryClient() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const queryCache = queryClient.getQueryCache();
  const add = (key: string, resourceDeps: string[]) => queryCache.build(queryClient, {
    queryKey: ["api", key],
    queryFn: async () => ({ key }),
    meta: { resourceDeps }
  }).setData({ key });
  add("overview", ["runs", "library"]);
  add("run:one", ["runs", "state"]);
  add("run:two", ["runs", "state"]);
  add("settings", ["settings"]);
  return queryClient;
}

function makeManager(fetcher: typeof fetch = vi.fn<typeof fetch>().mockResolvedValue(new Response('{"apiVersion":1,"version":"dev"}'))) {
  Object.defineProperty(window.navigator, "onLine", { configurable: true, value: true });
  const queryClient = makeQueryClient();
  const sockets: FakeSocket[] = [];
  const onUnauthorized = vi.fn();
  const onApiVersion = vi.fn();
  const manager = new EventConnectionManager({
    queryClient,
    onUnauthorized,
    onApiVersion,
    browser: window,
    socketFactory: () => { const socket = new FakeSocket(); sockets.push(socket); return socket as unknown as WebSocket; },
    fetcher
  });
  return { manager, queryClient, sockets, onUnauthorized, onApiVersion, fetcher };
}

afterEach(() => { vi.useRealTimers(); });

describe("one reconnecting event connection", () => {
  it("starts its timers with the global functions, not as its own methods, which browsers refuse", async () => {
    const real = globalThis.setTimeout;
    // A browser's setTimeout throws "Illegal invocation" when called with any other `this`; jsdom's does not, so this one does.
    vi.spyOn(globalThis, "setTimeout").mockImplementation(function (this: unknown, ...args: Parameters<typeof setTimeout>) {
      if (this !== undefined && this !== globalThis) throw new TypeError("Illegal invocation");
      return real(...args);
    } as typeof setTimeout);
    const { manager, queryClient, sockets } = makeManager();
    manager.start();
    sockets[0]!.send({ type: "hello", apiVersion: 1 });
    sockets[0]!.send({ type: "changed", resource: "runs", revision: 7 });
    // The refresh runs once its timer fires; a refused timer would leave the query current.
    await vi.waitFor(() => expect(queryClient.getQueryCache().find({ queryKey: ["api", "overview"] })?.state.isInvalidated).toBe(true));
    manager.stop();
  });

  it("invalidates dependent query variants and only those resources", () => {
    vi.useFakeTimers();
    const { manager, queryClient, sockets } = makeManager();
    manager.start();
    sockets[0]!.send({ type: "hello", apiVersion: 1 });
    sockets[0]!.send({ type: "changed", resource: "runs", revision: 7 });
    vi.advanceTimersByTime(150);
    const states = queryClient.getQueryCache().getAll().map(query => [query.queryKey[1], query.state.isInvalidated]);
    expect(states).toContainEqual(["overview", true]);
    expect(states).toContainEqual(["run:one", true]);
    expect(states).toContainEqual(["run:two", true]);
    expect(states).toContainEqual(["settings", false]);
    manager.stop();
  });

  it("shows offline, retries with a bounded delay, and invalidates every cached API query after reconnect", async () => {
    vi.useFakeTimers();
    const { manager, queryClient, sockets, fetcher } = makeManager();
    manager.start();
    sockets[0]!.send({ type: "hello", apiVersion: 1 });
    sockets[0]!.readyState = 3;
    sockets[0]!.dispatchEvent(new Event("close"));
    expect(manager.getSnapshot()).toBe("offline");
    expect(sockets).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(999);
    expect(sockets).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(sockets).toHaveLength(2);
    sockets[1]!.send({ type: "hello", apiVersion: 1 });
    expect(manager.getSnapshot()).toBe("connected");
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    expect(queryClient.getQueryCache().getAll().every(query => query.state.isInvalidated)).toBe(true);
    sockets[1]!.send({ type: "changed", resource: "runs", revision: 1 });
    expect(queryClient.getQueryCache().getAll().find(query => query.queryKey[1] === "run:one")?.state.isInvalidated).toBe(true);
    manager.stop();
    queryClient.clear();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("retries immediately on browser online, pauses while offline, and cleans up on stop", () => {
    vi.useFakeTimers();
    const { manager, queryClient, sockets } = makeManager();
    manager.start();
    expect(sockets).toHaveLength(1);
    Object.defineProperty(window.navigator, "onLine", { configurable: true, value: false });
    window.dispatchEvent(new Event("offline"));
    expect(manager.getSnapshot()).toBe("offline");
    queryClient.clear();
    expect(vi.getTimerCount()).toBe(0);
    Object.defineProperty(window.navigator, "onLine", { configurable: true, value: true });
    window.dispatchEvent(new Event("online"));
    expect(sockets).toHaveLength(2);
    manager.stop();
    queryClient.clear();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("ignores delayed events from a socket that was replaced after reconnect", () => {
    vi.useFakeTimers();
    const { manager, queryClient, sockets } = makeManager();
    manager.start();
    sockets[0]!.readyState = 3;
    sockets[0]!.dispatchEvent(new Event("close"));
    window.dispatchEvent(new Event("online"));
    expect(sockets).toHaveLength(2);
    sockets[0]!.send({ type: "hello", apiVersion: 2 });
    expect(manager.getSnapshot()).toBe("connecting");
    sockets[1]!.send({ type: "hello", apiVersion: 1 });
    sockets[0]!.send({ type: "changed", resource: "runs", revision: 1 });
    expect(manager.getSnapshot()).toBe("connected");
    expect(queryClient.getQueryCache().getAll().every(query => !query.state.isInvalidated)).toBe(true);
    manager.stop();
    queryClient.clear();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("enters sign-in after a rejected socket is checked over HTTP", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('{"message":"Unauthorized","problems":[]}', { status: 401 }));
    const { manager, sockets, onUnauthorized } = makeManager(fetcher);
    manager.start();
    sockets[0]!.readyState = 3;
    sockets[0]!.dispatchEvent(new Event("close"));
    await vi.waitFor(() => expect(onUnauthorized).toHaveBeenCalledTimes(1));
    expect(fetcher).toHaveBeenCalledWith("/api/v1/version", expect.objectContaining({ credentials: "same-origin" }));
    manager.stop();
  });

  it("blocks resource events for an incompatible API version and ignores build-only changes", async () => {
    const { manager, sockets, queryClient, onApiVersion } = makeManager();
    manager.start();
    sockets[0]!.send({ type: "hello", apiVersion: 2 });
    expect(manager.getSnapshot()).toBe("incompatible");
    sockets[0]!.send({ type: "changed", resource: "runs", revision: 1 });
    expect(queryClient.getQueryCache().getAll().every(query => !query.state.isInvalidated)).toBe(true);
    expect(onApiVersion).toHaveBeenCalledWith(2);
    manager.stop();

    const compatible = makeManager(vi.fn<typeof fetch>().mockResolvedValue(new Response('{"apiVersion":1,"version":"new-build"}')));
    compatible.manager.start();
    compatible.sockets[0]!.readyState = 3;
    compatible.sockets[0]!.dispatchEvent(new Event("close"));
    await vi.waitFor(() => expect(compatible.onApiVersion).toHaveBeenCalledWith(1));
    expect(compatible.manager.getSnapshot()).toBe("offline");
    compatible.manager.stop();
  });

  it("rejects malformed events and leaves no socket or retry timer after cleanup", () => {
    vi.useFakeTimers();
    const { manager, queryClient, sockets } = makeManager();
    manager.start();
    sockets[0]!.send({ type: "hello", apiVersion: "1" });
    expect(sockets[0]!.closeCalls[0]?.[0]).toBe(3002);
    manager.stop();
    queryClient.clear();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("coalesces a burst of changes into one refresh per window, and stop cancels a pending one", () => {
    vi.useFakeTimers();
    const { manager, sockets, queryClient } = makeManager();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    manager.start();
    sockets[0]!.send({ type: "hello", apiVersion: 1 });
    sockets[0]!.send({ type: "changed", resource: "runs", revision: 1 });
    sockets[0]!.send({ type: "changed", resource: "state", revision: 2 });
    sockets[0]!.send({ type: "changed", resource: "runs", revision: 3 });
    expect(invalidate).not.toHaveBeenCalled();
    vi.advanceTimersByTime(150);
    expect(invalidate).toHaveBeenCalledTimes(1);
    sockets[0]!.send({ type: "changed", resource: "runs", revision: 4 });
    manager.stop();
    vi.advanceTimersByTime(150);
    expect(invalidate).toHaveBeenCalledTimes(1);
    queryClient.clear();
  });

  it("ignores notifications without closing the socket or refreshing data, and keeps resource changes live", () => {
    vi.useFakeTimers();
    const { manager, sockets, queryClient } = makeManager();
    manager.start();
    sockets[0]!.send({ type: "hello", apiVersion: 1 });
    sockets[0]!.send({ type: "notification", title: "First", body: "A <notice>" });
    sockets[0]!.send({ type: "notification", title: "Second", body: "Two" });
    expect(manager.getSnapshot()).toBe("connected");
    expect(sockets).toHaveLength(1);
    expect(sockets[0]!.closeCalls).toEqual([]);
    expect(queryClient.getQueryCache().getAll().some(query => query.state.isInvalidated)).toBe(false);
    sockets[0]!.send({ type: "changed", resource: "runs", revision: 1 });
    vi.advanceTimersByTime(150);
    expect(queryClient.getQueryCache().getAll().find(query => query.queryKey[1] === "run:one")?.state.isInvalidated).toBe(true);
    expect(queryClient.getQueryCache().getAll().find(query => query.queryKey[1] === "settings")?.state.isInvalidated).toBe(false);
    manager.stop();
    queryClient.clear();
  });

  it("never creates a browser notification or asks for permission, even when the browser has granted it", () => {
    const construct = vi.fn();
    const requestPermission = vi.fn(async () => "granted" as NotificationPermission);
    class GrantedNotification {
      static permission = "granted";
      static requestPermission = requestPermission;
      constructor(title: string, options?: NotificationOptions) { construct(title, options); }
    }
    vi.stubGlobal("Notification", GrantedNotification);
    const { manager, sockets, queryClient } = makeManager();
    manager.start();
    sockets[0]!.send({ type: "hello", apiVersion: 1 });
    sockets[0]!.send({ type: "notification", title: "Run needs you", body: "Triage" });
    expect(construct).not.toHaveBeenCalled();
    expect(requestPermission).not.toHaveBeenCalled();
    expect(manager.getSnapshot()).toBe("connected");
    expect(sockets).toHaveLength(1);
    manager.stop();
    queryClient.clear();
  });
});
