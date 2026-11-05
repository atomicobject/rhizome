// Renders labelled before/after pairs as single PNGs for sharing in chat or a PR.
// Usage: node scripts/design-loop/pairs.mjs <before shots> <after shots> <out dir> <file> [<file> ...]
// Each <file> is a screenshot name present in both folders, e.g. explorer--light--1440.png.
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { chromium } from "@playwright/test";

const [before, after, out, ...files] = process.argv.slice(2);

if (!files.length) throw new Error("usage: pairs.mjs <before> <after> <out> <file>...");

mkdirSync(out, { recursive: true });

const browser = await chromium.launch();

const page = await browser.newPage({ viewport: { width: 1460, height: 520 } });

for (const file of files) {
  const cell = (label, dir) =>
    `<figure><figcaption>${label}</figcaption><img src="file://${path.resolve(dir, file)}"></figure>`;

  const html = `<style>body{margin:0;padding:10px;background:#e7e4df;display:flex;gap:10px;font:600 15px Inter,system-ui}figure{margin:0}figcaption{padding:0 0 6px}img{width:715px;border:1px solid #bbb}</style>${cell("Before: main", before)}${cell("After", after)}`;
  const sheet = path.resolve(out, `${path.basename(file, ".png")}.html`);

  writeFileSync(sheet, html);
  await page.goto(`file://${sheet}`);
  await page.waitForLoadState("load");
  await page.screenshot({ path: path.resolve(out, file), fullPage: true });
}

await browser.close();

console.log(`wrote ${files.length} pairs to ${out}`);
