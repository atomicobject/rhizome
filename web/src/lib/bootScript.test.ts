import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { themeBootstrapPlugin } from "../../themeBootstrap";

const html = themeBootstrapPlugin.transformIndexHtml.handler(
  readFileSync(resolve(__dirname, "../../index.html"), "utf8"),
);

const boot = /<script>([\s\S]*?)<\/script>/.exec(html)?.[1] ?? "";

function runBoot() {
  // oxlint-disable-next-line no-new-func -- executes the actual inline script produced for index.html
  new Function(boot)();
}

function desktop(baked: string, bakedAt: number) {
  Object.assign(window, {
    __RHIZOME_DESKTOP__: true,
    __RHIZOME_DESKTOP_THEME__: baked,
    __RHIZOME_DESKTOP_THEME_AT__: bakedAt,
  });
}

describe("the synchronous appearance boot script", () => {
  beforeEach(() => {
    localStorage.clear();
    delete document.documentElement.dataset.theme;
    delete document.documentElement.dataset.appearance;
    Object.assign(window, {
      __RHIZOME_DESKTOP__: false,
      __RHIZOME_DESKTOP_THEME__: undefined,
      __RHIZOME_DESKTOP_THEME_AT__: undefined,
    });
  });

  afterEach(() => vi.restoreAllMocks());

  it("paints the window's theme, or a newer one the page took from the app", () => {
    desktop("graphite", 100);
    runBoot();
    expect(document.documentElement.dataset.theme).toBe("graphite");

    localStorage.setItem("rhizome:desktop-theme", JSON.stringify({ theme: "spruce", at: 200 }));
    runBoot();
    expect(document.documentElement.dataset.theme).toBe("spruce");

    desktop("indigo", 300);
    runBoot();
    expect(document.documentElement.dataset.theme).toBe("indigo");
  });

  it("leaves browser choices to the desktop host", () => {
    localStorage.setItem("rhizome:theme", "slate");
    localStorage.setItem("rhizome:appearance", "dark");
    desktop("oxblood", 100);
    runBoot();
    expect(document.documentElement.dataset.theme).toBe("oxblood");
    expect(document.documentElement.dataset.appearance).toBeUndefined();
  });

  it("paints stored browser appearance and theme before React loads", () => {
    localStorage.setItem("rhizome:theme", "slate");
    localStorage.setItem("rhizome:appearance", "dark");
    runBoot();
    expect(document.documentElement.dataset.theme).toBe("slate");
    expect(document.documentElement.dataset.appearance).toBe("dark");
  });

  it.each(["auto", "invalid", ""])(
    "keeps CSS defaults for %s appearance and invalid theme",
    (appearance) => {
      localStorage.setItem("rhizome:theme", "invalid");
      localStorage.setItem("rhizome:appearance", appearance);
      runBoot();
      expect(document.documentElement.dataset.theme).toBeUndefined();
      expect(document.documentElement.dataset.appearance).toBeUndefined();
    },
  );

  it.each([false, true])("keeps CSS defaults when storage is denied (desktop: %s)", (isDesktop) => {
    if (isDesktop) desktop("spruce", 100);
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("Storage is denied", "SecurityError");
    });
    expect(runBoot).not.toThrow();
    expect(document.documentElement.dataset.theme).toBeUndefined();
    expect(document.documentElement.dataset.appearance).toBeUndefined();
  });

  it("keeps the desktop fallback when stored JSON cannot be read", () => {
    desktop("spruce", 100);
    localStorage.setItem("rhizome:desktop-theme", "invalid JSON");
    expect(runBoot).not.toThrow();
    expect(document.documentElement.dataset.theme).toBeUndefined();
  });
});
