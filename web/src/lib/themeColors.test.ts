import { afterEach, describe, expect, it } from "vitest";

import { resolveCssColor } from "./themeColors";

describe("resolveCssColor", () => {
  afterEach(() => document.documentElement.style.removeProperty("--test-ink"));

  it("reads a token's value and passes concrete colors through", () => {
    document.documentElement.style.setProperty("--test-ink", "#123456");

    expect(resolveCssColor("var(--test-ink)")).toBe("#123456");
    expect(resolveCssColor("#abcdef")).toBe("#abcdef");
    expect(resolveCssColor("var(--missing-token)", "#000000")).toBe("#000000");
  });
});
