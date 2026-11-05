// Compares two rounds' screenshots and writes a before/after contact sheet. Run from web/.
//   node scripts/design-loop/compare.mjs <before-shots> <after-shots> [--sheet <file.html>] [--all]
// Prints each screenshot as same (identical pixels), DIFF (with the count of changed pixels), or missing.
// The sheet shows the changed screens side by side (--all: every screen); it is written next to the
// after round unless --sheet names a file.
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { parseArgs } from "node:util";
import { chromium } from "@playwright/test";

const { values: args, positionals } = parseArgs({
  allowPositionals: true,
  options: { sheet: { type: "string" }, all: { type: "boolean", default: false } },
});

const [before, after] = positionals.map((dir) => path.resolve(dir));

if (!after)
  throw new Error("usage: compare.mjs <before-shots> <after-shots> [--sheet file.html] [--all]");

const sheet = path.resolve(args.sheet ?? path.join(after, "..", "compare.html"));

const manifest = async (dir) => JSON.parse(await readFile(path.join(dir, "manifest.json"), "utf8"));

const entries = new Map();

for (const [side, dir] of [
  ["before", before],
  ["after", after],
])
  for (const shot of await manifest(dir)) {
    const entry = entries.get(shot.file) ?? { ...shot };
    entry[side] = path.join(dir, shot.file);
    entries.set(shot.file, entry);
  }

// Decode in the browser so PNG encoding differences never count as changes.
const browser = await chromium.launch();

const page = await browser.newPage();

const changedPixels = (a, b) =>
  page.evaluate(
    async ([a, b]) => {
      const pixels = async (b64) => {
        const image = await createImageBitmap(
          await (await fetch(`data:image/png;base64,${b64}`)).blob(),
        );

        const canvas = new OffscreenCanvas(image.width, image.height);
        canvas.getContext("2d").drawImage(image, 0, 0);

        return {
          w: image.width,
          h: image.height,
          d: canvas.getContext("2d").getImageData(0, 0, image.width, image.height).data,
        };
      };

      const [x, y] = await Promise.all([pixels(a), pixels(b)]);

      if (x.w !== y.w || x.h !== y.h) return -1;
      let n = 0;

      for (let i = 0; i < x.d.length; i += 4)
        if (
          x.d[i] !== y.d[i] ||
          x.d[i + 1] !== y.d[i + 1] ||
          x.d[i + 2] !== y.d[i + 2] ||
          x.d[i + 3] !== y.d[i + 3]
        )
          n++;

      return n;
    },
    [a.toString("base64"), b.toString("base64")],
  );

const counts = { same: 0, diff: 0, missing: 0 };

for (const entry of entries.values()) {
  if (!entry.before || !entry.after) {
    entry.status = "missing";
  } else {
    const [a, b] = await Promise.all([readFile(entry.before), readFile(entry.after)]);
    entry.changed = a.equals(b) ? 0 : await changedPixels(a, b);
    entry.status = entry.changed === 0 ? "same" : "diff";
  }

  counts[entry.status]++;

  if (entry.status !== "same")
    console.log(
      entry.status === "diff" ? "DIFF" : "missing",
      entry.file,
      entry.changed === -1 ? "size changed" : (entry.changed ?? ""),
    );
}

await browser.close();

console.log(`${counts.same} same, ${counts.diff} different, ${counts.missing} missing`);

const escape = (text) => String(text).replace(/[&<>"]/g, (c) => `&#${c.charCodeAt(0)};`);

const img = (file) =>
  file
    ? `<img src="${escape(path.relative(path.dirname(sheet), file))}" loading="lazy">`
    : "<p>missing</p>";

const shown = [...entries.values()].filter((e) => args.all || e.status !== "same");

await writeFile(
  sheet,
  `<!doctype html><meta charset="utf-8"><title>Design loop compare</title>
<style>body{margin:16px;font:14px system-ui,sans-serif;background:#f4f4f4;color:#222}h2{font-size:15px;margin:24px 0 6px}
.pair{display:grid;grid-template-columns:1fr 1fr;gap:8px}figure{margin:0}figcaption{color:#555;margin-bottom:4px}img{width:100%;border:1px solid #ccc}</style>
<h1>Design loop compare</h1><p>Before: ${escape(before)}<br>After: ${escape(after)}<br>${counts.same} same, ${counts.diff} different, ${counts.missing} missing</p>
${shown
  .map(
    (
      e,
    ) => `<h2>${escape(e.title)} · ${e.theme} · ${e.width} <small>(${e.status}${e.changed > 0 ? `, ${e.changed} px` : ""})</small></h2>
<div class="pair"><figure><figcaption>Before</figcaption>${img(e.before)}</figure><figure><figcaption>After</figcaption>${img(e.after)}</figure></div>`,
  )
  .join("\n")}
`,
);

console.log("sheet", sheet);
