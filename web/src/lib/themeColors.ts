// Canvas renderers (Sigma's WebGL) need concrete colors, so they read token
// values here and re-read them when the appearance or theme changes. Tokens
// are light-dark() pairs, so a token's raw text is not a color: resolve it by
// painting a probe element and reading its computed color.

import { subscribeAppearance } from "./appearance";

const cache = new Map<string, string>();

let probe: HTMLElement | null = null;

// Registered before any subscriber, so their reads see the new appearance.
subscribeAppearance(() => cache.clear());

function computed(token: string): string {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(token).trim();

  // A literal color needs no probe; light-dark(), color-mix(), and var() do.
  if (!raw || !raw.includes("(") || /^(rgb|hsl)a?\(/.test(raw)) return raw;

  if (!probe || !probe.isConnected) {
    probe = document.createElement("span");
    probe.setAttribute("aria-hidden", "true");
    probe.style.cssText = "position:absolute;width:0;height:0;overflow:hidden;visibility:hidden";
    document.body.append(probe);
  }

  probe.style.color = `var(${token})`;

  return getComputedStyle(probe).color;
}

/**
 * Resolve `var(--token)` to the token's current color. Any other color passes
 * through; `fallback` covers an unset token (tests).
 */
export function resolveCssColor(value: string, fallback = "#8a8682"): string {
  const token = /^var\((--[\w-]+)\)$/.exec(value.trim())?.[1];

  if (!token) return value;

  let resolved = cache.get(token);

  if (resolved === undefined) {
    resolved = computed(token);
    cache.set(token, resolved);
  }

  return resolved || fallback;
}

/** Call `onChange` after the appearance or theme switches; returns the unsubscribe function. */
export function subscribeColorScheme(onChange: () => void): () => void {
  return subscribeAppearance(onChange);
}
