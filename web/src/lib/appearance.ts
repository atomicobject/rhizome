// Appearance (light, dark, or match the system) and the chrome theme. Both are
// attributes on <html> that tokens.css reads. In a browser the choice persists
// per origin in localStorage, applied before first paint by the inline script
// in index.html; Rhizome Desktop owns both instead (it sets the app appearance
// natively and sends the theme as a page command), so the page ignores
// storage there.

import { useSyncExternalStore } from "react";

import { DEFAULT_THEME, isThemeName, type ThemeName } from "./themes";

export type Appearance = "auto" | "light" | "dark";

const APPEARANCE_KEY = "rhizome:appearance";

const THEME_KEY = "rhizome:theme";

function isAppearance(value: string | null | undefined): value is Appearance {
  return value === "auto" || value === "light" || value === "dark";
}

// The desktop app marks its content view with an initialization script.
function inDesktop(): boolean {
  return window.__RHIZOME_DESKTOP__ === true;
}

function root(): HTMLElement {
  return document.documentElement;
}

function stored(key: string): string | null {
  try {
    return globalThis.localStorage?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function store(key: string, value: string): void {
  try {
    globalThis.localStorage?.setItem(key, value);
  } catch {
    // Storage can be unavailable (private mode, quota); the choice then lasts
    // for this page only.
  }
}

export function currentAppearance(): Appearance {
  const value = root().dataset.appearance;

  return isAppearance(value) ? value : "auto";
}

export function currentTheme(): ThemeName {
  const value = root().dataset.theme;

  return isThemeName(value) ? value : DEFAULT_THEME;
}

const darkScheme = globalThis.matchMedia?.("(prefers-color-scheme: dark)") ?? null;

/** The appearance the page renders in: an explicit choice, else the system's. */
export function resolvedAppearance(): "light" | "dark" {
  const appearance = currentAppearance();

  if (appearance !== "auto") return appearance;

  return darkScheme?.matches ? "dark" : "light";
}

export function applyAppearance(appearance: Appearance): void {
  if (appearance === "auto") delete root().dataset.appearance;
  else root().dataset.appearance = appearance;
}

export function applyTheme(theme: ThemeName): void {
  root().dataset.theme = theme;
}

/** Choose an appearance and remember it for this origin (browser only). */
export function setAppearance(appearance: Appearance): void {
  applyAppearance(appearance);

  if (!inDesktop()) store(APPEARANCE_KEY, appearance);
}

/** Choose a chrome theme and remember it for this origin (browser only). */
export function setTheme(theme: ThemeName): void {
  applyTheme(theme);

  if (!inDesktop()) store(THEME_KEY, theme);
}

/** Re-read the stored choice, as after another tab changed it. */
export function applyStored(): void {
  if (inDesktop()) return;

  const appearance = stored(APPEARANCE_KEY);
  const theme = stored(THEME_KEY);

  applyAppearance(isAppearance(appearance) ? appearance : "auto");
  applyTheme(isThemeName(theme) ? theme : DEFAULT_THEME);
}

/**
 * Call `onChange` whenever the rendered appearance or theme may have changed:
 * an explicit choice, the system switching, or another tab's choice. Returns
 * the unsubscribe function.
 */
export function subscribeAppearance(onChange: () => void): () => void {
  const observer = new MutationObserver(onChange);

  observer.observe(root(), {
    attributes: true,
    attributeFilter: ["data-appearance", "data-theme"],
  });
  darkScheme?.addEventListener("change", onChange);

  const onStorage = (event: StorageEvent) => {
    if (event.key === APPEARANCE_KEY || event.key === THEME_KEY) applyStored();
  };

  globalThis.addEventListener?.("storage", onStorage);

  return () => {
    observer.disconnect();
    darkScheme?.removeEventListener("change", onChange);
    globalThis.removeEventListener?.("storage", onStorage);
  };
}

export type AppearanceState = {
  appearance: Appearance;
  theme: ThemeName;
  resolved: "light" | "dark";
};

let snapshot: AppearanceState | null = null;

function readSnapshot(): AppearanceState {
  const next = {
    appearance: currentAppearance(),
    theme: currentTheme(),
    resolved: resolvedAppearance(),
  };

  if (
    snapshot &&
    snapshot.appearance === next.appearance &&
    snapshot.theme === next.theme &&
    snapshot.resolved === next.resolved
  ) {
    return snapshot;
  }

  snapshot = next;

  return next;
}

/** The current appearance, theme, and resolved light or dark, kept live. */
export function useAppearance(): AppearanceState {
  return useSyncExternalStore(subscribeAppearance, readSnapshot, readSnapshot);
}
