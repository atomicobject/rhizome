/** Categorical type colors in tokens.css: `--type-1` … `--type-8`. */
const TYPE_COLOR_COUNT = 8;

/** Stable palette slot (1–8) for a type name, so a type keeps one color on every surface. */
export function typeAccentIndex(name: string): number {
  let hash = 0;

  for (let i = 0; i < name.length; i++) {
    hash = (hash * 31 + name.charCodeAt(i)) >>> 0;
  }

  return (hash % TYPE_COLOR_COUNT) + 1;
}

/** A type's color as a CSS value. It marks dots, borders, and edges, never text. */
export function typeAccentColor(name: string): string {
  return `var(--type-${typeAccentIndex(name)})`;
}
