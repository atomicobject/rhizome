// @vitest-environment node

import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it } from "vitest";

import { decodeJson, isJsonValue, type JsonValue } from "./src/api/parse";

import { createRhizomeDevProxyMiddleware, resolveRhizomeDevApiTarget } from "./devProxyConfig";

describe("resolveRhizomeDevApiTarget", () => {
  const tempDirs: string[] = [];

  afterEach(() => {
    for (const dir of tempDirs) {
      fs.rmSync(dir, { recursive: true, force: true });
    }

    tempDirs.length = 0;
  });

  it("prefers the explicit env override", () => {
    const root = makeTempDir(tempDirs);
    fs.mkdirSync(path.join(root, ".rhizome"));
    fs.writeFileSync(
      path.join(root, ".rhizome", "runtime.json"),
      JSON.stringify({ HTTPURL: "http://127.0.0.1:8787" }),
    );
    expect(
      resolveRhizomeDevApiTarget({
        cwd: root,
        env: { VITE_RHIZOME_API_ORIGIN: "http://127.0.0.1:9321/api" },
      }),
    ).toBe("http://127.0.0.1:9321");
  });

  it("finds the nearest worktree serve discovery file", () => {
    const root = makeTempDir(tempDirs);
    const nested = path.join(root, "web", "src");
    fs.mkdirSync(path.join(root, ".rhizome"), { recursive: true });
    fs.mkdirSync(path.join(root, "web", ".rhizome"), { recursive: true });
    fs.mkdirSync(nested, { recursive: true });
    fs.writeFileSync(
      path.join(root, ".rhizome", "runtime.json"),
      JSON.stringify({ HTTPURL: "http://127.0.0.1:8787" }),
    );
    fs.writeFileSync(
      path.join(root, "web", ".rhizome", "runtime.json"),
      JSON.stringify({ HTTPURL: "http://127.0.0.1:8788" }),
    );

    expect(resolveRhizomeDevApiTarget({ cwd: nested, env: {} })).toBe("http://127.0.0.1:8788");
  });

  it("returns null when there is no override or discovery file", () => {
    const root = makeTempDir(tempDirs);

    expect(resolveRhizomeDevApiTarget({ cwd: root, env: {} })).toBeNull();
  });

  it("proxies HTML viewer hosts to the backend with the browser host intact", async () => {
    const root = makeTempDir(tempDirs);
    let forwardedHost: string | undefined;

    const backend = await listen((req, res) => {
      expect(req.url).toBe("/viewer/index.html");
      forwardedHost = req.headers.host;
      res.setHeader("content-type", "text/html");
      res.end("<p>viewer</p>");
    });

    const proxy = await listen(
      connectHandler(createRhizomeDevProxyMiddleware({ cwd: root, env: {} })),
    );

    fs.mkdirSync(path.join(root, ".rhizome"), { recursive: true });
    fs.writeFileSync(
      path.join(root, ".rhizome", "runtime.json"),
      JSON.stringify({ HTTPURL: backend.origin }),
    );
    const proxyPort = new URL(proxy.origin).port;

    try {
      // fetch() refuses a custom Host header, so use http.request directly.
      const body = await new Promise<string>((resolve, reject) => {
        const request = http.request(
          `${proxy.origin}/viewer/index.html`,
          { headers: { host: `abc123.localhost:${proxyPort}` } },
          (response) => {
            let text = "";
            response.setEncoding("utf8");
            response.on("data", (chunk: string) => (text += chunk));
            response.on("end", () => resolve(text));
          },
        );

        request.on("error", reject);
        request.end();
      });

      expect(body).toBe("<p>viewer</p>");
      expect(forwardedHost).toBe(`abc123.localhost:${proxyPort}`);
    } finally {
      await Promise.all([backend.close(), proxy.close()]);
    }
  });

  it("forwards api requests to the current discovered backend", async () => {
    const root = makeTempDir(tempDirs);
    let forwardedOrigin: string | undefined;
    let forwardedHost: string | undefined;

    const backend = await listen((req, res) => {
      expect(req.url).toBe("/api/status?fresh=true");
      forwardedOrigin = req.headers.origin;
      forwardedHost = req.headers.host;
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ ok: true }));
    });

    const proxy = await listen(
      connectHandler(createRhizomeDevProxyMiddleware({ cwd: root, env: {} })),
    );

    fs.mkdirSync(path.join(root, ".rhizome"), { recursive: true });
    fs.writeFileSync(
      path.join(root, ".rhizome", "runtime.json"),
      JSON.stringify({ HTTPURL: backend.origin }),
    );

    try {
      await expect(
        fetchJSON(`${proxy.origin}/api/status?fresh=true`, { origin: proxy.origin }),
      ).resolves.toEqual({ ok: true });
      // Origin and Host stay the browser's: the backend derives the
      // application origin and the HTML viewer origin from them.
      expect(forwardedOrigin).toBe(proxy.origin);
      expect(forwardedHost).toBe(new URL(proxy.origin).host);
    } finally {
      await Promise.all([backend.close(), proxy.close()]);
    }
  });

  it("forwards custom view and kit requests to the backend", async () => {
    const root = makeTempDir(tempDirs);
    const forwarded: string[] = [];

    const backend = await listen((req, res) => {
      forwarded.push(req.url ?? "");
      res.end("backend");
    });

    const proxy = await listen(
      connectHandler(createRhizomeDevProxyMiddleware({ cwd: root, env: {} })),
    );

    fs.mkdirSync(path.join(root, ".rhizome"), { recursive: true });
    fs.writeFileSync(
      path.join(root, ".rhizome", "runtime.json"),
      JSON.stringify({ HTTPURL: backend.origin }),
    );

    try {
      for (const target of ["/views/poc.board", "/views/_files/poc/board.tsx", "/kit/v1/boot.js"]) {
        await expect((await fetch(`${proxy.origin}${target}`)).text()).resolves.toBe("backend");
      }

      expect(forwarded).toEqual([
        "/views/poc.board",
        "/views/_files/poc/board.tsx",
        "/kit/v1/boot.js",
      ]);
    } finally {
      await Promise.all([backend.close(), proxy.close()]);
    }
  });

  it("returns json when an api request has no backend to proxy to", async () => {
    const root = makeTempDir(tempDirs);

    const proxy = await listen(
      connectHandler(createRhizomeDevProxyMiddleware({ cwd: root, env: {} })),
    );

    try {
      const response = await fetch(`${proxy.origin}/api/agent/sessions`);

      await expect(response.json()).resolves.toEqual({
        error:
          "Rhizome backend is not available. Start it with `make dev` or set VITE_RHIZOME_API_ORIGIN.",
      });
      expect(response.status).toBe(503);
      expect(response.headers.get("content-type")).toContain("application/json");
    } finally {
      await proxy.close();
    }
  });
});

function makeTempDir(tempDirs: string[]): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rhizome-vite-proxy-"));
  tempDirs.push(dir);

  return dir;
}

function listen(
  handler: http.RequestListener,
): Promise<{ origin: string; close: () => Promise<void> }> {
  const server = http.createServer(handler);

  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();

      if (!isTCPAddress(address)) {
        throw new Error("expected TCP server address");
      }

      resolve({
        origin: `http://127.0.0.1:${address.port}`,
        close: () =>
          new Promise((closeResolve, closeReject) => {
            server.close((error) => (error ? closeReject(error) : closeResolve()));
          }),
      });
    });
  });
}

function connectHandler(
  handler: ReturnType<typeof createRhizomeDevProxyMiddleware>,
): http.RequestListener {
  return (req, res) => {
    handler(req, res, () => {
      res.statusCode = 404;
      res.end("not found");
    });
  };
}

function isTCPAddress(value: string | AddressInfo | null): value is AddressInfo {
  return value !== null && typeof value !== "string";
}

async function fetchJSON(url: string, headers?: Record<string, string>): Promise<JsonValue | null> {
  const response = await fetch(url, { headers });

  return decodeJson(await response.text(), isJsonValue);
}
