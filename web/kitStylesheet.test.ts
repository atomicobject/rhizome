// @vitest-environment node

import { Scanner } from "@tailwindcss/oxide";
import path from "node:path";
import { describe, expect, it } from "vitest";

import { buildKitStylesheet, kitCandidates } from "./kitStylesheet";

const starters = path.resolve(import.meta.dirname, "../pkg/app/cli/init/templates/starters");

describe("kit stylesheet", () => {
  it("scans every bundled starter view, including the AO KB views", async () => {
    const kit = new Set(await kitCandidates());

    for (const starter of ["ao-kb", "agentic-engineering", "compounding"]) {
      const views = new Scanner({
        sources: [
          { base: path.join(starters, starter, "module/views"), pattern: "**/*", negated: false },
        ],
      }).scan();

      expect(views.length).toBeGreaterThan(0);
      expect(views.filter((candidate) => !kit.has(candidate))).toEqual([]);
    }
  });

  it("precompiles the theme, the kit's components, and the views' utilities", async () => {
    const css = await buildKitStylesheet();

    // Theme tokens, a kit component, and utilities the AO KB views use.
    const rules = [
      "--surface:",
      ".rz-section-head",
      ".rz-markdown-mark",
      ".markdown h2",
      ".hljs-keyword",
      ".mermaid-block",
      ".text-muted-foreground{",
      ".text-sm{",
    ];

    expect(rules.filter((rule) => !css.includes(rule))).toEqual([]);
    expect(["@source", "@apply", "@theme"].filter((rule) => css.includes(rule))).toEqual([]);
  });
});
