import fs from "node:fs";
import http from "node:http";
import https from "node:https";
import path from "node:path";
import type { Connect, Plugin } from "vite";

type DevProxyEnv = Record<string, string | undefined>;

type ResolveRhizomeDevApiTargetOptions = {
  cwd?: string;
  env?: DevProxyEnv;
};

type ServeDiscoveryManifest = {
  HTTPURL?: string;
  httpURL?: string;
};

const OVERRIDE_ENV = "VITE_RHIZOME_API_ORIGIN";

const DISCOVERY_FILE = path.join(".rhizome", "runtime.json");

export function rhizomeDevProxyPlugin(options: ResolveRhizomeDevApiTargetOptions = {}): Plugin {
  return {
    name: "rhizome-dev-proxy",
    configureServer(server) {
      server.middlewares.use(createRhizomeDevProxyMiddleware(options));
    },
  };
}

export function createRhizomeDevProxyMiddleware(
  options: ResolveRhizomeDevApiTargetOptions = {},
): Connect.NextHandleFunction {
  return (req, res, next) => {
    if (!isRhizomeBackendPath(req.url) && !isHTMLViewerHost(req.headers.host)) {
      next();

      return;
    }

    const target = resolveRhizomeDevApiTarget(options);

    if (!target) {
      writeProxyError(
        res,
        503,
        "Rhizome backend is not available. Start it with `make dev` or set VITE_RHIZOME_API_ORIGIN.",
      );

      return;
    }

    proxyToRhizome(req, res, target);
  };
}

function isRhizomeBackendPath(url: string | undefined): boolean {
  if (!url) return false;
  const pathname = url.split("?", 1)[0] || "/";

  // Custom views and their kit are served by the Go server, never by Vite.
  return (
    pathname === "/api" ||
    pathname.startsWith("/api/") ||
    pathname.startsWith("/views/") ||
    pathname.startsWith("/kit/")
  );
}

// The HTML note viewer serves each document from `<token>.localhost:<port>`,
// where the port is the one the browser used to reach the app. In dev that is
// Vite's port, so those hosts must reach the backend through this proxy too.
function isHTMLViewerHost(host: string | undefined): boolean {
  if (!host) return false;
  const hostname = host.replace(/:\d+$/, "").toLowerCase();

  return hostname.endsWith(".localhost") && hostname !== "localhost";
}

export function resolveRhizomeDevApiTarget(
  options: ResolveRhizomeDevApiTargetOptions = {},
): string | null {
  const env = options.env ?? process.env;
  const override = normalizeOrigin(env[OVERRIDE_ENV]);

  if (override) return override;

  const startDir = path.resolve(options.cwd ?? process.cwd());
  const discoveryPath = findDiscoveryPath(startDir);

  if (!discoveryPath) return null;

  try {
    const raw = fs.readFileSync(discoveryPath, "utf8");
    const parsed: unknown = JSON.parse(raw);

    if (!isServeDiscoveryManifest(parsed)) return null;

    return normalizeOrigin(parsed.HTTPURL ?? parsed.httpURL);
  } catch {
    return null;
  }
}

function isServeDiscoveryManifest(value: unknown): value is ServeDiscoveryManifest {
  if (typeof value !== "object" || value === null) return false;

  if ("HTTPURL" in value && typeof value.HTTPURL !== "string") return false;

  if ("httpURL" in value && typeof value.httpURL !== "string") return false;

  return true;
}

function findDiscoveryPath(startDir: string): string | null {
  let current = startDir;

  for (;;) {
    const candidate = path.join(current, DISCOVERY_FILE);

    if (fs.existsSync(candidate)) return candidate;
    const parent = path.dirname(current);

    if (parent === current) return null;
    current = parent;
  }
}

function normalizeOrigin(value: string | undefined): string | null {
  if (!value) return null;

  try {
    return new URL(value).origin;
  } catch {
    return null;
  }
}

function proxyToRhizome(
  req: Connect.IncomingMessage,
  res: http.ServerResponse,
  targetOrigin: string,
): void {
  const target = new URL(req.url ?? "/", targetOrigin);
  const transport = target.protocol === "https:" ? https : http;
  // Forward Host and Origin untouched. The backend derives the application
  // origin (its mutation origin check and the HTML viewer's frame-ancestors)
  // and the `<token>.localhost:<port>` viewer origin from them, so they must
  // name the Vite origin the browser is actually on.
  const headers = { ...req.headers };

  const proxyReq = transport.request(
    target,
    {
      method: req.method,
      headers,
    },
    (proxyRes) => {
      res.writeHead(proxyRes.statusCode ?? 502, proxyRes.headers);
      proxyRes.pipe(res);
    },
  );

  proxyReq.on("error", (error) => {
    if (res.headersSent) {
      res.destroy(error);

      return;
    }

    writeProxyError(res, 502, `Rhizome backend proxy failed: ${error.message}`);
  });

  req.pipe(proxyReq);
}

function writeProxyError(res: http.ServerResponse, status: number, message: string): void {
  res.statusCode = status;
  res.setHeader("content-type", "application/json");
  res.end(JSON.stringify({ error: message }));
}
