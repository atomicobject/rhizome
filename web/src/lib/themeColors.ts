// Canvas renderers (Sigma's WebGL) need concrete colors, so they read token
// values here and re-read them when the operating system switches themes.

const cache = new Map<string, string>();

const darkScheme = globalThis.matchMedia?.("(prefers-color-scheme: dark)") ?? null;

// Registered before any subscriber, so their reads see the new theme.
darkScheme?.addEventListener("change", () => cache.clear());

/**
 * Resolve `var(--token)` to the token's current value on the document root.
 * Any other color passes through; `fallback` covers an unset token (tests).
 */
export function resolveCssColor(value: string, fallback = "#8a8682"): string {
  const token = /^var\((--[\w-]+)\)$/.exec(value.trim())?.[1];

  if (!token) return value;

  let resolved = cache.get(token);

  if (resolved === undefined) {
    resolved = getComputedStyle(document.documentElement).getPropertyValue(token).trim();
    cache.set(token, resolved);
  }

  return resolved || fallback;
}

/** Call `onChange` after the theme switches; returns the unsubscribe function. */
export function subscribeColorScheme(onChange: () => void): () => void {
  darkScheme?.addEventListener("change", onChange);

  return () => darkScheme?.removeEventListener("change", onChange);
}
