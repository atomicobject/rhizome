export const APPEARANCES = ["auto", "light", "dark"] as const;

export type Appearance = (typeof APPEARANCES)[number];

export const APPEARANCE_KEY = "rhizome:appearance";

export const THEME_KEY = "rhizome:theme";

export const DESKTOP_THEME_KEY = "rhizome:desktop-theme";

export function isAppearance(value: string | null | undefined): value is Appearance {
  return APPEARANCES.some((appearance) => appearance === value);
}
