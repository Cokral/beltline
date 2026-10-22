import type { QueryClient } from "@tanstack/react-query";
import { apiRequest } from "./client";
import { queryResources } from "./resources";
import { isEventMessage, supportedApiVersion } from "./events";
import type { EventResource, VersionResponse } from "./wire.gen";

export type ConnectionStatus = "connecting" | "connected" | "offline" | "incompatible";

export interface EventConnectionOptions {
  queryClient: QueryClient;
  onUnauthorized: () => void;
  onApiVersion: (version: number) => void;
  browser?: Window;
  socketFactory?: (url: string) => WebSocket;
  fetcher?: typeof fetch;
  setTimer?: SetTimer;
  clearTimer?: ClearTimer;
}

/** Starts a timer. The defaults call the global functions directly: stored as a method, `setTimeout` would run with the manager as `this`, which browsers refuse. */
type SetTimer = (callback: () => void, delay?: number) => ReturnType<typeof setTimeout>;
type ClearTimer = (timer: ReturnType<typeof setTimeout>) => void;

const retryDelays = [1000, 2000, 4000, 8000] as const;

// Changes that arrive within this window refresh their queries together. The daemon sends one
// change per turn, and a tick can make many turns, so a burst would otherwise refetch the same
// views several times a second. The window bounds how late a refresh can be.
const refreshWindowMs = 150;

function socketUrl(browser: Window): string {
  const scheme = browser.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${browser.location.host}/api/v1/events`;
}

export class EventConnectionManager {
  private readonly browser: Window;
  private readonly makeSocket: (url: string) => WebSocket;
  private readonly setTimer: SetTimer;
  private readonly clearTimer: ClearTimer;
  private socket: WebSocket | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private started = false;
  private stopped = false;
  private connectedBefore = false;
  private retries = 0;
  private revisions = new Map<EventResource, number>();
  private pendingResources = new Set<EventResource>();
  private refreshTimer: ReturnType<typeof setTimeout> | null = null;
  private listeners = new Set<() => void>();
  private status: ConnectionStatus = "connecting";

  constructor(private readonly options: EventConnectionOptions) {
    this.browser = options.browser ?? window;
    this.makeSocket = options.socketFactory ?? (url => new WebSocket(url));
    this.setTimer = options.setTimer ?? ((callback, delay) => setTimeout(callback, delay));
    this.clearTimer = options.clearTimer ?? (timer => clearTimeout(timer));
  }

  readonly getSnapshot = (): ConnectionStatus => this.status;

  readonly subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  start(): void {
    if (this.started) return;
    this.started = true;
    this.stopped = false;
    this.browser.addEventListener("offline", this.handleOffline);
    this.browser.addEventListener("online", this.handleOnline);
    if (!this.browser.navigator.onLine) this.setStatus("offline");
    else this.connect();
  }

  stop(): void {
    this.stopped = true;
    this.started = false;
    this.browser.removeEventListener("offline", this.handleOffline);
    this.browser.removeEventListener("online", this.handleOnline);
    if (this.timer !== null) this.clearTimer(this.timer);
    this.timer = null;
    if (this.refreshTimer !== null) this.clearTimer(this.refreshTimer);
    this.refreshTimer = null;
    this.pendingResources.clear();
    const socket = this.socket;
    this.socket = null;
    socket?.close(1000, "client stopped");
    this.revisions.clear();
  }

  private connect(): void {
    if (this.stopped || !this.started || this.status === "incompatible" || !this.browser.navigator.onLine) return;
    if (this.socket && (this.socket.readyState === 0 || this.socket.readyState === 1)) return;
    this.setStatus("connecting");
    this.revisions.clear();
    try {
      const socket = this.makeSocket(socketUrl(this.browser));
      this.socket = socket;
      socket.addEventListener("message", this.handleMessage);
      socket.addEventListener("close", this.handleClose);
      socket.addEventListener("error", this.handleError);
    } catch {
      void this.checkVersion();
      this.scheduleReconnect();
    }
  }

  private readonly handleMessage = (event: MessageEvent): void => {
    if (event.currentTarget !== this.socket) return;
    if (typeof event.data !== "string") {
      this.socket?.close(3002, "expected JSON text");
      return;
    }
    let decoded: unknown;
    try { decoded = JSON.parse(event.data) as unknown; }
    catch {
      this.socket?.close(3002, "invalid JSON");
      return;
    }
    if (!isEventMessage(decoded)) {
      this.socket?.close(3002, "invalid event contract");
      return;
    }
    const message = decoded;
    if (message.type === "hello") {
      this.options.onApiVersion(message.apiVersion);
      if (message.apiVersion !== supportedApiVersion) {
        this.setStatus("incompatible");
        this.socket?.close(3002, "unsupported API version");
        return;
      }
      if (this.connectedBefore) {
        void this.options.queryClient.invalidateQueries({ predicate: query => query.queryKey[0] === "api" });
        void this.checkVersion();
      }
      this.connectedBefore = true;
      this.retries = 0;
      this.setStatus("connected");
      return;
    }
    if (this.status !== "connected") return;
    // Notifications are delivered by the daemon's macOS notices; the browser only refreshes on resource changes.
    if (message.type !== "changed") return;
    const previous = this.revisions.get(message.resource);
    if (previous !== undefined && message.revision <= previous) return;
    this.revisions.set(message.resource, message.revision);
    this.pendingResources.add(message.resource);
    this.scheduleRefresh();
  };

  private scheduleRefresh(): void {
    if (this.refreshTimer !== null) return;
    this.refreshTimer = this.setTimer(() => this.flushRefresh(), refreshWindowMs);
  }

  private flushRefresh(): void {
    this.refreshTimer = null;
    const resources = this.pendingResources;
    this.pendingResources = new Set();
    if (resources.size === 0) return;
    void this.options.queryClient.invalidateQueries({
      predicate: query => queryResources(query.meta).some(resource => resources.has(resource))
    });
  }

  private readonly handleClose = (event: Event): void => {
    if (event.currentTarget !== this.socket) return;
    if (this.stopped || this.status === "incompatible") return;
    this.socket = null;
    this.setStatus("offline");
    void this.checkVersion();
    this.scheduleReconnect();
  };

  private readonly handleError = (event: Event): void => {
    if (event.currentTarget !== this.socket) return;
    if (this.stopped || this.status === "incompatible") return;
    this.setStatus("offline");
    void this.checkVersion();
    const socket = this.socket;
    this.socket = null;
    socket?.close(3003, "event socket failed");
    this.scheduleReconnect();
  };

  private readonly handleOffline = (): void => {
    this.setStatus("offline");
    if (this.timer !== null) this.clearTimer(this.timer);
    this.timer = null;
    const socket = this.socket;
    this.socket = null;
    socket?.close(1001, "browser offline");
  };

  private readonly handleOnline = (): void => {
    if (this.timer !== null) this.clearTimer(this.timer);
    this.timer = null;
    if (this.status !== "incompatible") this.connect();
  };

  private scheduleReconnect(): void {
    if (this.stopped || this.timer !== null || !this.browser.navigator.onLine || this.status === "incompatible") return;
    this.setStatus("offline");
    const delay = retryDelays[Math.min(this.retries, retryDelays.length - 1)] ?? retryDelays[retryDelays.length - 1];
    this.retries += 1;
    this.timer = this.setTimer(() => {
      this.timer = null;
      this.connect();
    }, delay);
  }

  private async checkVersion(): Promise<void> {
    if (this.stopped) return;
    try {
      const version = await apiRequest<VersionResponse>("/api/v1/version", {
        fetcher: this.options.fetcher,
        onUnauthorized: this.options.onUnauthorized
      });
      this.options.onApiVersion(version.apiVersion);
      if (version.apiVersion !== supportedApiVersion) this.setStatus("incompatible");
    } catch {
      // Network failures keep the offline banner; 401 is surfaced by the callback.
    }
  }

  private setStatus(status: ConnectionStatus): void {
    if (status === this.status) return;
    this.status = status;
    for (const listener of this.listeners) listener();
  }
}
