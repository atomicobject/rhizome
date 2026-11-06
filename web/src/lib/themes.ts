// The preset chrome themes. Free of dependencies, so Rhizome Desktop's shell
// shares this list without the web UI's packages.

/** Preset chrome themes; `swatch` is the light sidebar color for pickers. */
export const THEMES = [
  { id: "graphite", label: "Graphite", swatch: "#45433f" },
  { id: "slate", label: "Slate", swatch: "#3d4654" },
  { id: "spruce", label: "Spruce", swatch: "#2f4a47" },
  { id: "oxblood", label: "Oxblood", swatch: "#573538" },
  { id: "indigo", label: "Indigo", swatch: "#3c3d63" },
] as const;

export type ThemeName = (typeof THEMES)[number]["id"];

export const DEFAULT_THEME: ThemeName = "graphite";

export function isThemeName(value: string | null | undefined): value is ThemeName {
  return THEMES.some((theme) => theme.id === value);
}
