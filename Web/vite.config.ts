import { copyFileSync, mkdirSync, readdirSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";
import type { Plugin, ProxyOptions } from "vite";

function copyFontLicences(): Plugin {
  const configDirectory = fileURLToPath(new URL(".", import.meta.url));
  const source = resolve(configDirectory, "src/theme/fonts");
  const destination = resolve(configDirectory, "dist/fonts");
  return {
    name: "beltline-font-licences",
    apply: "build",
    closeBundle() {
      mkdirSync(destination, { recursive: true });
      for (const file of readdirSync(source).filter(name => name.startsWith("OFL-") && name.endsWith(".txt"))) {
        copyFileSync(resolve(source, file), resolve(destination, file));
      }
    }
  };
}

export function daemonTarget(environment: NodeJS.ProcessEnv = process.env): URL {
  if (environment.BELTLINE_DAEMON_ORIGIN) {
    const target = new URL(environment.BELTLINE_DAEMON_ORIGIN);
    if (target.protocol !== "http:" || target.hostname !== "127.0.0.1" || target.username || target.password || target.pathname !== "/" || target.search || target.hash) {
      throw new Error("BELTLINE_DAEMON_ORIGIN must be a bare http://127.0.0.1 origin");
    }
    return target;
  }
  const port = environment.BELTLINE_DAEMON_PORT ?? "7331";
  if (!/^\d+$/.test(port) || Number(port) < 1 || Number(port) > 65535) {
    throw new Error("BELTLINE_DAEMON_PORT must be a valid TCP port");
  }
  return new URL(`http://127.0.0.1:${port}`);
}

export default defineConfig(() => {
  const target = daemonTarget();
  const proxy: ProxyOptions = {
    target: target.origin,
    changeOrigin: true,
    configure(proxyServer) {
      proxyServer.on("proxyReq", (request, incomingRequest) => {
        request.setHeader("host", target.host);
        if (incomingRequest.headers.origin) request.setHeader("origin", target.origin);
      });
      proxyServer.on("proxyReqWs", (request, incomingRequest) => {
        request.setHeader("host", target.host);
        if (incomingRequest.headers.origin) request.setHeader("origin", target.origin);
      });
    }
  };
  return {
    plugins: [copyFontLicences()],
    server: {
      host: "127.0.0.1",
      proxy: {
        "/api": { ...proxy, ws: true },
        "/auth": proxy,
        "/artifacts": proxy
      }
    },
    preview: { host: "127.0.0.1" },
    build: { outDir: "dist", emptyOutDir: true },
    // A generous timeout: tests that pass alone in a second time out at 5 s when several suites share the machine.
    test: { environment: "jsdom", testTimeout: 20_000, setupFiles: ["./src/test/setup.ts"], restoreMocks: true, css: { include: [/components\/components\.css$/] }, exclude: ["**/node_modules/**", "**/dist/**", "snapshots/**/*.spec.ts"] }
  };
});
