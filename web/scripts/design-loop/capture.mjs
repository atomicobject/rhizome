// Captures the design-loop screenshot set. Run from web/; see README.md.
//   node scripts/design-loop/capture.mjs --vault <dir> --out <dir> [--rzm <bin>] [--port 7841]
//   node scripts/design-loop/capture.mjs --base http://127.0.0.1:7841 --out <dir>   (an already-running server)
//   Optional: --only id,id  --themes light,dark  --now ISO  --jobs 4
// With --vault, the script starts the server with fresh view state, waits for indexing, captures, and stops it.
// --now pins the browser clock so relative times ("3d") match across rounds.
// Each shot gets a fresh browser context, a seeded Math.random, and no server-side writes that later shots read.
import { spawn } from "node:child_process";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { chromium } from "@playwright/test";

const here = path.dirname(fileURLToPath(import.meta.url));

const { values: args } = parseArgs({
  options: {
    base: { type: "string" },
    vault: { type: "string" },
    rzm: { type: "string", default: path.resolve(here, "../../../bin", process.platform, "rzm") },
    port: { type: "string", default: "7841" },
    out: { type: "string" },
    only: { type: "string" },
    themes: { type: "string", default: "light,dark" },
    now: { type: "string" },
    jobs: { type: "string", default: "4" },
  },
});

if (!args.out || !(args.base || args.vault))
  throw new Error("--out and one of --vault or --base are required");

const view = (id, kind) =>
  `presentation=${encodeURIComponent(`view:${JSON.stringify([id, kind])}`)}`;

const spec = "docs/specs/experience/workbench-redesign.md";

const htmlEffort = "docs/efforts/2026-09-13-09-55-html-effort-pilot/2026-09-13-09-55-effort.html";

// Global cross-section: the screens scored for the app overall (shell, header pattern, type, tokens).
export const GLOBAL = [
  "all-briefing",
  "type-table",
  "type-table-record",
  "type-board",
  "group-overview",
  "note-read",
  "search",
  "ontology",
];

// `widths` defaults to [1440]; add 1100 where the layout changes at laptop width.
// An `act` runs after the page settles; the page settles again before the screenshot.
export const SURFACES = [
  {
    id: "all-briefing",
    title: "All notes · Briefing",
    url: `/notes?${view("workspace.briefing", "custom")}`,
  },
  {
    id: "all-overview",
    title: "All notes · Overview",
    url: `/notes?${view("workspace.overview", "custom")}`,
  },
  {
    id: "type-briefing",
    title: "Type (Efforts) · Briefing",
    url: `/notes/effort?${view("interface.briefing", "custom")}`,
  },
  {
    id: "type-table",
    title: "Type (Efforts) · Table",
    url: `/notes/effort?${view("generated.interface.Effort.table", "table")}`,
    widths: [1440, 1100],
  },
  {
    id: "type-table-record",
    title: "Type (Efforts) · Table with Record pane",
    url: `/notes/effort?${view("generated.interface.Effort.table", "table")}`,
    widths: [1440, 1100],
    act: async (p) => {
      await p.locator(".configured-view:visible tr.configured-view__row").nth(1).click();
      // The pane opens on selection at 1200px and up; below that it opens over the table from the toggle.
      const toggle = p.locator(".notes-shell__pane-toggle:enabled");
      await toggle.waitFor();

      if ((await toggle.getAttribute("aria-expanded")) !== "true") await toggle.click();
      await p.locator("#notes-right-pane").waitFor();
    },
  },
  {
    id: "type-cards",
    title: "Type (Specs) · Cards",
    url: `/notes/spec-like?${view("generated.interface.SpecLike.table", "card")}`,
  },
  {
    id: "type-board",
    title: "Type (Efforts) · Board",
    url: `/notes/effort?${view("generated.interface.Effort.table", "kanban")}`,
  },
  {
    id: "group-briefing",
    title: "Group (Delivery) · Briefing",
    url: `/notes/group/Delivery?${view("group.briefing", "custom")}`,
  },
  {
    id: "group-overview",
    title: "Group (Delivery) · Overview",
    url: `/notes/group/Delivery?${view("group.overview", "custom")}`,
  },
  {
    id: "group-trace",
    title: "Group (Delivery) · Trace",
    url: `/notes/group/Delivery?${view("group.trace", "custom")}`,
  },
  {
    id: "group-sections",
    title: "Group (Delivery) · Sections",
    url: `/notes/group/Delivery?${view("group.sections", "custom")}`,
  },
  {
    id: "action-items",
    title: "Action Items (configured view)",
    url: "/notes?view=action-items.open",
  },
  { id: "problems", title: "Problems (validation)", url: "/notes/issues" },
  {
    id: "note-read",
    title: "Note · read mode (spec)",
    url: `/notes?note=${encodeURIComponent(spec)}`,
    widths: [1440, 1100],
  },
  {
    id: "note-edit",
    title: "Note · edit mode (spec)",
    url: `/notes?note=${encodeURIComponent(spec)}`,
    act: async (p) => {
      await p.getByRole("button", { name: "Edit", exact: true }).first().click();
    },
  },
  {
    id: "html-note",
    title: "HTML note (effort)",
    url: `/notes?note=${encodeURIComponent(htmlEffort)}`,
  },
  { id: "search", title: "Search results", url: "/notes?search=typed+retrieval" },
  {
    id: "finder",
    title: "Finder (⌘P)",
    url: `/notes?${view("workspace.briefing", "custom")}`,
    act: async (p) => {
      await p.keyboard.press("Meta+p");
    },
  },
  { id: "ontology", title: "Ontology · Atlas", url: "/ontology" },
  { id: "ontology-type", title: "Ontology · type detail (Effort)", url: "/ontology/type/Effort" },
  { id: "explorer", title: "Explorer (graph)", url: "/explorer" },
  { id: "agent", title: "Agent workspace", url: "/agent" },
  { id: "tasks", title: "Tasks", url: "/tasks" },
  { id: "graphql", title: "GraphQL explorer", url: "/graphql" },
];

// Runs in every frame before the app. Seeds Math.random (the graph layout jitters its starting
// positions), counts DOM changes, and counts running graph-layout workers.
function instrument() {
  let seed = 0x9e3779b9;
  Math.random = () => {
    seed = (seed + 0x6d2b79f5) | 0;
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;

    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };

  window.__changes = 0;
  new MutationObserver(() => window.__changes++).observe(document, {
    subtree: true,
    childList: true,
    attributes: true,
    characterData: true,
  });
  window.__layouts = 0;
  const NativeWorker = window.Worker;

  if (!NativeWorker) return;
  // A layout is running from construction until its result or error arrives, or the app cancels it.
  window.Worker = class extends NativeWorker {
    #running = false;

    constructor(url, options) {
      super(url, options);

      if (!String(url).includes("graphLayout")) return;
      this.#running = true;
      window.__layouts++;
      // Registered before the app's handler, so the count drops just before the layout installs.
      this.addEventListener("message", () => this.#done());
      this.addEventListener("error", () => this.#done());
    }

    #done() {
      if (this.#running) window.__layouts--;
      this.#running = false;
    }

    terminate() {
      this.#done();
      super.terminate();
    }
  };
}

// One frame's readiness: "busy" while anything is still loading, otherwise its DOM change count.
function frameState() {
  const busy =
    window.__layouts > 0 ||
    document.readyState !== "complete" ||
    document.fonts.status !== "loaded" ||
    [...document.images].some((img) => !img.complete) ||
    document.querySelector('[aria-busy="true"]');

  return busy ? "busy" : String(window.__changes);
}

const QUIET_MS = 500;

const SETTLE_TIMEOUT_MS = 20000;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Settled: no requests in flight and every frame idle and unchanged for QUIET_MS.
async function settle(page, net, label) {
  const start = Date.now();
  let last = "";
  let since = start;

  while (Date.now() - start < SETTLE_TIMEOUT_MS) {
    const state = (
      await Promise.all(page.frames().map((f) => f.evaluate(frameState).catch(() => "gone")))
    ).join();

    const now = Date.now();

    if (
      net.inflight.size ||
      state.includes("busy") ||
      state !== last ||
      now - net.last < QUIET_MS
    ) {
      last = state;
      since = now;
    } else if (now - since >= QUIET_MS) {
      return;
    }

    await sleep(50);
  }

  const pending = [...net.inflight].map((r) => r.url().replace(/^https?:\/\/[^/]+/, ""));
  console.log("SLOW", label, "did not settle in", SETTLE_TIMEOUT_MS, "ms", pending.join(" "));
}

// The server's index generation when it is idle, or null while it indexes or validates. Something
// outside the capture can touch the vault and start a re-index; a shot taken then shows changing counts.
async function indexGeneration(base) {
  const status = await fetch(`${base}/api/v1/status`)
    .then((r) => (r.ok ? r.json() : null))
    .catch(() => null);

  const live = status?.live;

  // A fresh server reports ready before its startup re-index begins, so also wait for a finished epoch.
  const idle =
    status?.indexState === "ready" &&
    status.validation?.status === "ok" &&
    live?.lastCompletedEpoch &&
    !live.currentEpoch &&
    live.pendingDirtyCount === 0 &&
    !live.scheduler?.embeddingRunning &&
    !live.scheduler?.graphPending &&
    !live.scheduler?.graphRunning;

  return idle ? `${live.lastCompletedEpoch.id}/${status.validation.generation}` : null;
}

async function waitForIdle(base, isAlive = () => true) {
  for (let i = 0; i < 600; i++) {
    if (!isAlive()) throw new Error("server exited");
    const generation = await indexGeneration(base);

    if (generation) return generation;
    await sleep(250);
  }

  throw new Error("server did not finish indexing in 150 s");
}

async function startServer() {
  const server = spawn(path.join(here, "snapshot.sh"), ["serve", args.vault, args.port, args.rzm], {
    stdio: "ignore",
  });

  const exited = new Promise((resolve) => server.once("exit", resolve));
  const base = `http://127.0.0.1:${args.port}`;

  try {
    await waitForIdle(base, () => server.exitCode === null);
  } catch (error) {
    server.kill();
    throw new Error(`${error.message}; run snapshot.sh serve by hand to see its output`);
  }

  return { base, stop: () => (server.kill(), exited) };
}

const started = Date.now();

const only = args.only ? new Set(args.only.split(",")) : null;

const shots = SURFACES.filter((s) => !only || only.has(s.id)).flatMap((s) =>
  args.themes
    .split(",")
    .flatMap((theme) => (s.widths ?? [1440]).map((width) => ({ s, theme, width }))),
);

await mkdir(args.out, { recursive: true });

// Partial raster repaints only invalidated tile areas, so antialiased edges depended on what had
// repainted before; without it two captures of the same build match byte for byte.
const browser = await chromium.launch({ args: ["--disable-partial-raster"] });

let server;

const results = [];

let failed = 0;

// Takes one screenshot in a fresh browser context and returns the PNG.
async function shoot({ s, theme, width }, file) {
  const context = await browser.newContext({
    viewport: { width, height: 900 },
    deviceScaleFactor: 1,
    colorScheme: theme,
    reducedMotion: "reduce",
  });

  await context.addInitScript(instrument);
  // Finder recents are the server state one shot leaves for another; the app ignores a failed record.
  await context.route("**/api/v1/find/opens", (route) => route.fulfill({ status: 204 }));
  // The agent screen shows the local CLI version and signed-in account; pin both so shots match across machines
  // and the account never reaches reviewers.
  await context.route("**/api/agent/settings", async (route) => {
    if (route.request().method() !== "GET") return route.continue();
    const response = await route.fetch();
    const body = await response.json();

    for (const { status } of body.harnesses ?? []) {
      if (status.version) status.version = "1.0.0";

      if (status.account) status.account = "reviewer@example.com";
    }

    await route.fulfill({ response, json: body });
  });
  const page = await context.newPage();

  if (args.now) await page.clock.setFixedTime(new Date(args.now));
  const net = { inflight: new Set(), last: Date.now() };
  page.on("request", (r) => {
    const type = r.resourceType();

    if (type === "eventsource" || type === "websocket") return;
    net.inflight.add(r);
    net.last = Date.now();

    if (r.method() !== "GET" && r.url().includes("/api/v1/view-preferences"))
      console.log("WARN", file, "saved view state that other shots may read:", r.method(), r.url());
  });

  const finished = (r) => {
    net.inflight.delete(r);
    net.last = Date.now();
  };

  page.on("requestfinished", finished);
  page.on("requestfailed", finished);

  try {
    await page.goto(server.base + s.url, { waitUntil: "load" });
    await settle(page, net, file);

    if (s.act) {
      await s.act(page);
      await settle(page, net, file);
    }

    return await page.screenshot({ animations: "disabled" });
  } finally {
    await context.close();
  }
}

// Retakes a shot when the server re-indexed while it was taken.
async function capture(shot) {
  const file = `${shot.s.id}--${shot.theme}--${shot.width}.png`;

  try {
    for (let attempt = 1; ; attempt++) {
      const before = await waitForIdle(server.base);
      const png = await shoot(shot, file);
      const steady = (await indexGeneration(server.base)) === before;

      if (steady || attempt === 3) {
        if (!steady) console.log("WARN", file, "server kept re-indexing; kept the last attempt");
        await writeFile(path.join(args.out, file), png);
        break;
      }

      console.log("RETRY", file, "server re-indexed during the shot");
    }

    console.log("ok  ", file);
    const { id, title } = shot.s;

    return { id, title, theme: shot.theme, width: shot.width, file, global: GLOBAL.includes(id) };
  } catch (error) {
    failed++;
    console.log("FAIL", file, error.message.split("\n")[0]);

    return null;
  }
}

try {
  server = args.vault ? await startServer() : { base: args.base, stop: async () => {} };
  let next = 0;

  const worker = async () => {
    while (next < shots.length) {
      const i = next++;
      results[i] = await capture(shots[i]);
    }
  };

  await Promise.all(Array.from({ length: Number(args.jobs) }, worker));
} finally {
  await browser.close();
  await server?.stop();
}

const manifest = results.filter(Boolean);

await writeFile(path.join(args.out, "manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`);

console.log(
  `${manifest.length} shots, ${failed} failed, ${((Date.now() - started) / 1000).toFixed(1)} s`,
);

if (failed) process.exitCode = 1;
