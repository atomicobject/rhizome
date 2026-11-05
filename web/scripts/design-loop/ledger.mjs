// Builds a round's score table from the reviewers' JSON files.
// Usage: node scripts/design-loop/ledger.mjs rounds/round-N [rounds/round-(N-1)]
// Reads <round>/scores/*.json ({ overall, screens } per the rubric), writes <round>/LEDGER.md, and prints it.
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

const [dir, prevDir] = process.argv.slice(2);

if (!dir) throw new Error("usage: ledger.mjs <round dir> [previous round dir]");

const KEYS = [
  "typography",
  "hierarchy",
  "spacing_density",
  "color_contrast",
  "iconography",
  "consistency_polish",
  "clarity",
  "overall",
];

function load(round) {
  const scores = path.join(round, "scores");

  return Object.fromEntries(
    readdirSync(scores)
      .filter((f) => f.endsWith(".json"))
      .map((f) => [
        path.basename(f, ".json"),
        JSON.parse(readFileSync(path.join(scores, f), "utf8")),
      ]),
  );
}

const mean = (xs) => xs.reduce((a, b) => a + b, 0) / xs.length;

const cur = load(dir);

const prev = prevDir ? load(prevDir) : null;

const names = Object.keys(cur).sort();

const screens = Object.keys(cur[names[0]].screens);

const prevMean = (screen) => {
  if (!prev) return null;

  const vals = Object.values(prev)
    .map((r) => (screen ? r.screens[screen]?.overall : r.overall.overall))
    .filter((v) => v !== undefined);

  return vals.length ? mean(vals) : null;
};

const row = (label, vals, before) => {
  const m = mean(vals);

  const delta =
    before === null
      ? ""
      : ` ${before.toFixed(2)} | ${(m - before >= 0 ? "+" : "") + (m - before).toFixed(2)} |`;

  return {
    m,
    line: `| ${label} | ${vals.map((v) => v.toFixed(1)).join(" | ")} | ${m.toFixed(2)} | ${Math.min(...vals).toFixed(1)} |${delta}`,
  };
};

const rows = screens.map((s) => ({
  s,
  ...row(
    s,
    names.map((n) => cur[n].screens[s].overall),
    prevMean(s),
  ),
}));

rows.sort((a, b) => a.m - b.m);

const head = `| Screen | ${names.join(" | ")} | Mean | Min |${prev ? " Prev | Δ |" : ""}`;

const rule = `|${"---|".repeat(names.length + 3 + (prev ? 2 : 0))}`;

const app = row(
  "**App overall**",
  names.map((n) => cur[n].overall.overall),
  prevMean(null),
);

const cells = names.flatMap((n) => screens.flatMap((s) => KEYS.map((k) => cur[n].screens[s][k])));

const lowest = names
  .flatMap((n) => screens.flatMap((s) => KEYS.map((k) => ({ n, s, k, v: cur[n].screens[s][k] }))))
  .sort((a, b) => a.v - b.v)
  .slice(0, 10)
  .map((c) => `${c.s} ${c.k} (${c.n} ${c.v})`)
  .join("; ");

const out = [
  `# ${path.basename(dir)}`,
  "",
  head,
  rule,
  ...rows.map((r) => r.line),
  app.line,
  "",
  `Cells at 9 or more: ${cells.filter((v) => v >= 9).length} of ${cells.length}.`,
  `Lowest cells: ${lowest}.`,
].join("\n");

writeFileSync(path.join(dir, "LEDGER.md"), `${out}\n`);

console.log(out);
