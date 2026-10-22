// @vitest-environment node
import { createHash, randomBytes } from "node:crypto";
import { once } from "node:events";
import { createServer as createHttpServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { createConnection, createServer as createTcpServer } from "node:net";
import type { AddressInfo } from "node:net";
import { resolve } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { createServer as createViteServer, type ViteDevServer } from "vite";
import { daemonTarget } from "../../vite.config";

interface ForwardedRequest { path: string; host?: string; origin?: string; upgrade: boolean }

let daemon: Server;
let vite: ViteDevServer;
let daemonPort = 0;
let vitePort = 0;
const received: ForwardedRequest[] = [];
const upgrades: Array<{ path: string; host?: string; origin?: string }> = [];

function listen(server: Server): Promise<void> {
  return new Promise((resolveListen, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => { server.off("error", reject); resolveListen(); });
  });
}

function close(server: Server): Promise<void> {
  return new Promise((resolveClose, reject) => server.close(error => error ? reject(error) : resolveClose()));
}

async function availablePort(): Promise<number> {
  const probe = createTcpServer();
  await new Promise<void>((resolveListen, reject) => {
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => { probe.off("error", reject); resolveListen(); });
  });
  const port = (probe.address() as AddressInfo).port;
  await new Promise<void>((resolveClose, reject) => probe.close(error => error ? reject(error) : resolveClose()));
  return port;
}

function sendResponse(request: IncomingMessage, response: ServerResponse): void {
  const record: ForwardedRequest = { path: request.url ?? "", host: request.headers.host, origin: request.headers.origin, upgrade: false };
  received.push(record);
  if (request.url?.startsWith("/auth")) {
    response.writeHead(302, { location: "/", "set-cookie": "beltline_auth=opaque; Path=/api; HttpOnly; SameSite=Strict", "cache-control": "no-store" });
    response.end();
    return;
  }
  if (request.url?.startsWith("/artifacts/")) {
    response.writeHead(200, { "content-type": "text/html; charset=utf-8", "x-artifact-content": "preserved" });
    response.end("<main>artifact</main>");
    return;
  }
  response.writeHead(200, { "content-type": "application/json" });
  response.end('{"apiVersion":1,"version":"proxy-test"}');
}

beforeAll(async () => {
  daemon = createHttpServer(sendResponse);
  daemon.on("upgrade", (request, socket) => {
    upgrades.push({ path: request.url ?? "", host: request.headers.host, origin: request.headers.origin });
    const key = request.headers["sec-websocket-key"] ?? "";
    const accept = createHash("sha1").update(`${key}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`).digest("base64");
    socket.write(`HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`);
    setTimeout(() => socket.destroy(), 20);
  });
  await listen(daemon);
  daemonPort = (daemon.address() as AddressInfo).port;
  process.env.BELTLINE_DAEMON_PORT = String(daemonPort);
  vite = await createViteServer({
    configFile: resolve(process.cwd(), "vite.config.ts"),
    logLevel: "silent",
    server: { host: "127.0.0.1", port: await availablePort(), strictPort: true }
  });
  await vite.listen();
  vitePort = (vite.httpServer!.address() as AddressInfo).port;
});

afterAll(async () => {
  await vite?.close();
  await close(daemon);
  delete process.env.BELTLINE_DAEMON_PORT;
});

describe("Vite same-origin daemon proxy", () => {
  it("targets only the daemon loopback authority", () => {
    expect(daemonTarget({ BELTLINE_DAEMON_PORT: "7444" }).origin).toBe("http://127.0.0.1:7444");
    expect(() => daemonTarget({ BELTLINE_DAEMON_ORIGIN: "http://localhost:7331" })).toThrow(/127\.0\.0\.1/);
    expect(() => daemonTarget({ BELTLINE_DAEMON_ORIGIN: "http://127.0.0.1:7331/prefix" })).toThrow(/bare/);
    expect(() => daemonTarget({ BELTLINE_DAEMON_ORIGIN: "http://user@127.0.0.1:7331" })).toThrow(/bare/);
    expect(() => daemonTarget({ BELTLINE_DAEMON_PORT: "not-a-port" })).toThrow(/valid TCP port/);
  });

  it("rewrites HTTP Host and supplied Origin, preserving auth, cookie, redirect, and artifact headers", async () => {
    const browserOrigin = `http://127.0.0.1:${vitePort}`;
    const version = await fetch(`${browserOrigin}/api/v1/version`, { headers: { origin: browserOrigin } });
    expect(await version.json()).toEqual({ apiVersion: 1, version: "proxy-test" });
    const auth = await fetch(`${browserOrigin}/auth?token=local`, { redirect: "manual", headers: { origin: browserOrigin } });
    const artifact = await fetch(`${browserOrigin}/artifacts/run-1/attempt-1/trace.html`, { headers: { origin: browserOrigin } });
    expect(auth.status).toBe(302);
    expect(auth.headers.get("location")).toBe("/");
    expect(auth.headers.get("set-cookie")).toContain("Path=/api");
    expect(artifact.headers.get("content-type")).toContain("text/html");
    expect(artifact.headers.get("x-artifact-content")).toBe("preserved");
    expect(await artifact.text()).toContain("artifact");
    for (const entry of received.slice(-3)) {
      expect(entry.host).toBe(`127.0.0.1:${daemonPort}`);
      expect(entry.origin).toBe(`http://127.0.0.1:${daemonPort}`);
    }
    const deepLink = await fetch(`${browserOrigin}/runs/issues%3Aacme%2Fweb%2361`);
    expect(deepLink.status).toBe(200);
    expect(deepLink.headers.get("content-type")).toContain("text/html");
    expect(await deepLink.text()).toContain("/src/main.tsx");
  });

  it("rewrites WebSocket Host and Origin for event upgrades", async () => {
    const key = randomBytes(16).toString("base64");
    const browserOrigin = `http://127.0.0.1:${vitePort}`;
    const client = createConnection(vitePort, "127.0.0.1");
    const response = new Promise<string>((resolveResponse, reject) => {
      const timeout = setTimeout(() => reject(new Error("WebSocket proxy handshake timed out")), 4000);
      client.on("data", chunk => {
        const text = chunk.toString("utf8");
        if (text.includes("101 Switching Protocols")) { clearTimeout(timeout); resolveResponse(text); client.destroy(); }
      });
      client.on("error", error => { clearTimeout(timeout); reject(error); });
    });
    await once(client, "connect");
    client.write([
      "GET /api/v1/events HTTP/1.1",
      `Host: 127.0.0.1:${vitePort}`,
      "Connection: Upgrade",
      "Upgrade: websocket",
      `Origin: ${browserOrigin}`,
      `Sec-WebSocket-Key: ${key}`,
      "Sec-WebSocket-Version: 13",
      "\r\n"
    ].join("\r\n"));
    const handshake = await response;
    expect(handshake).toContain("101 Switching Protocols");
    expect(upgrades.at(-1)).toMatchObject({ path: "/api/v1/events", host: `127.0.0.1:${daemonPort}`, origin: `http://127.0.0.1:${daemonPort}` });
  });
});
