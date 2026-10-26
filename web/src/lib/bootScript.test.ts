// The inline head script in index.html paints the theme before React loads.
// Run it as written, against a desktop window's globals.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

const html = readFileSync(resolve(__dirname, "../../index.html"), "utf8");

const boot = /<script>([\s\S]*?)<\/script>/.exec(html)?.[1] ?? "";

function runBoot(baked: string, bakedAt: number) {
  Object.assign(window, {
    __RHIZOME_DESKTOP__: true,
    __RHIZOME_DESKTOP_THEME__: baked,
    __RHIZOME_DESKTOP_THEME_AT__: bakedAt,
  });
  // oxlint-disable-next-line no-new-func -- runs the page's own inline script, read from index.html
  new Function(boot)();
}

describe("the desktop boot script", () => {
  afterEach(() => {
    localStorage.clear();
    delete document.documentElement.dataset.theme;
  });

  it("paints the window's theme, or a newer one the page took from the app", () => {
    runBoot("graphite", 100);
    expect(document.documentElement.dataset.theme).toBe("graphite");

    // After a theme change, a reload paints the theme the page remembered.
    localStorage.setItem("rhizome:desktop-theme", JSON.stringify({ theme: "spruce", at: 200 }));
    runBoot("graphite", 100);
    expect(document.documentElement.dataset.theme).toBe("spruce");

    // A window opened after that change has the newer theme baked in.
    runBoot("indigo", 300);
    expect(document.documentElement.dataset.theme).toBe("indigo");
  });
});
