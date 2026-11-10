export type Inline =
  | { kind: "text"; text: string }
  | { kind: "link"; label: string; path: string }
  | { kind: "unresolved"; label: string; target: string };

/** Only AO wikilinks are interpreted; all other content remains literal text. */
export function inlineText(
  text: string,
  resolve: (target: string) => string | undefined,
): Inline[] {
  const parts: Inline[] = [];
  let end = 0;
  for (const match of text.matchAll(/\[\[([^\[\]\n]+)\]\]/g)) {
    if (match.index > end) parts.push({ kind: "text", text: text.slice(end, match.index) });
    const [target, alias] = match[1].split("|");
    const label = alias?.trim() || target.trim();
    const path = resolve(target.trim());
    parts.push(
      path ? { kind: "link", label, path } : { kind: "unresolved", label, target: target.trim() },
    );
    end = match.index + match[0].length;
  }
  if (end < text.length) parts.push({ kind: "text", text: text.slice(end) });
  return parts;
}
