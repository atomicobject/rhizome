// WCAG AA for every chrome theme and both appearances, read straight from
// tokens.css so a token edit that breaks contrast fails here.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { THEMES } from "./themes";

const css = readFileSync(resolve(__dirname, "../tokens.css"), "utf8");

type Pair = [light: string, dark: string];

/** `--name: light-dark(#light, #dark)` declarations inside one rule body. */
function pairs(body: string): Map<string, Pair> {
  const found = new Map<string, Pair>();

  for (const match of body.matchAll(
    /(--[\w-]+):\s*light-dark\(\s*(#[0-9a-f]{6})\s*,\s*(#[0-9a-f]{6})\s*\)/gi,
  )) {
    found.set(match[1], [match[2], match[3]]);
  }

  return found;
}

function ruleBody(selector: RegExp): string {
  const start = css.search(selector);

  expect(start, `rule ${selector}`).toBeGreaterThanOrEqual(0);

  const open = css.indexOf("{", start);

  return css.slice(open + 1, css.indexOf("\n}", open));
}

const content = pairs(ruleBody(/^:root \{\n  \/\* ---- Atomic Object brand palette/m));

const chromeBase = pairs(ruleBody(/^:root \{\n  --chrome-text:/m));

const graphite = pairs(ruleBody(/^:root,\n:root\[data-theme="graphite"\]/m));

function theme(id: string): Map<string, Pair> {
  if (id === "graphite") return new Map([...chromeBase, ...graphite]);

  return new Map([
    ...chromeBase,
    ...pairs(ruleBody(new RegExp(`^:root\\[data-theme="${id}"\\]`, "m"))),
  ]);
}

function channel(hex: string, at: number): number {
  const value = Number.parseInt(hex.slice(at, at + 2), 16) / 255;

  return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;
}

function luminance(hex: string): number {
  return 0.2126 * channel(hex, 1) + 0.7152 * channel(hex, 3) + 0.0722 * channel(hex, 5);
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);

  return (hi + 0.05) / (lo + 0.05);
}

/** color-mix(in srgb, a p%, b), rounded like a browser would. */
function mix(a: string, b: string, p: number): string {
  let out = "#";

  for (const at of [1, 3, 5]) {
    const value = Math.round(
      Number.parseInt(a.slice(at, at + 2), 16) * p +
        Number.parseInt(b.slice(at, at + 2), 16) * (1 - p),
    );

    out += value.toString(16).padStart(2, "0");
  }

  return out;
}

function pick(map: Map<string, Pair>, name: string, side: 0 | 1): string {
  const value = map.get(name)?.[side];

  if (!value) throw new Error(`${name} is not a light-dark() pair in tokens.css`);

  return value;
}

const AA = 4.5;

describe("content tokens", () => {
  for (const [side, label] of [
    [0, "light"],
    [1, "dark"],
  ] as const) {
    it(`keep text, muted, and faint text at AA on every surface (${label})`, () => {
      for (const text of ["--text", "--text-muted", "--text-faint"]) {
        for (const surface of ["--surface", "--surface-sunk", "--surface-strong"]) {
          const ratio = contrast(pick(content, text, side), pick(content, surface, side));

          expect(ratio, `${text} on ${surface}`).toBeGreaterThanOrEqual(AA);
        }
      }
    });
  }
});

describe.each(THEMES.map((option) => option.id))("%s theme", (id) => {
  const tokens = theme(id);

  for (const [side, label] of [
    [0, "light"],
    [1, "dark"],
  ] as const) {
    it(`keeps chrome text and the active pill at AA (${label})`, () => {
      const text = pick(tokens, "--chrome-text", side);
      const muted = pick(tokens, "--chrome-muted", side);

      for (const ground of ["--chrome-bg", "--chrome-top"]) {
        const surface = pick(tokens, ground, side);

        expect(contrast(text, surface), `chrome text on ${ground}`).toBeGreaterThanOrEqual(AA);
        expect(contrast(muted, surface), `muted chrome text on ${ground}`).toBeGreaterThanOrEqual(
          AA,
        );
      }

      const active = mix(text, pick(tokens, "--chrome-bg", side), 0.16);

      expect(contrast(text, active), "chrome text on the active tab").toBeGreaterThanOrEqual(AA);

      // --chrome-raised: the global search field, whose placeholder is muted text.
      const raised = mix(text, pick(tokens, "--chrome-bg", side), 0.09);

      expect(
        contrast(muted, raised),
        "muted chrome text on the search field",
      ).toBeGreaterThanOrEqual(AA);
      expect(
        contrast(pick(tokens, "--chrome-pill-text", side), pick(tokens, "--chrome-pill", side)),
        "pill text on the pill",
      ).toBeGreaterThanOrEqual(AA);
    });

    it(`keeps faint text at AA on the selection tint (${label})`, () => {
      const selection =
        side === 0
          ? mix(pick(tokens, "--chrome-bg", 0), "#ffffff", 0.09)
          : mix(pick(tokens, "--chrome-pill", 1), "#1c1b1a", 0.45);

      expect(contrast(pick(content, "--text-faint", side), selection)).toBeGreaterThanOrEqual(AA);
    });
  }
});
