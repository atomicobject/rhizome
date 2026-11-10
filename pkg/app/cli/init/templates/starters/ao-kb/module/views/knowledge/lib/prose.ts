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

export function paragraphs(text: string) {
  return text
    .trim()
    .split(/\r?\n[ \t]*\r?\n+/)
    .filter(Boolean);
}

/** `text` with fenced code blocks and inline code spans masked character for character, so
 * nothing inside them reads as a link while every line keeps its length and blankness and
 * `paragraphs` splits the result exactly as it splits `text`. */
export function maskCode(text: string) {
  const mask = (s: string) => s.replace(/\S/g, "_");
  let fence = "";
  const lines = text.split("\n").map((line) => {
    const marker = /^ {0,3}(`{3,}|~{3,})/.exec(line)?.[1];
    if (!fence) {
      if (!marker) return line;
      fence = marker;
    } else if (marker?.[0] === fence[0] && marker.length >= fence.length && /^\s*[`~]+\s*$/.test(line)) {
      fence = "";
    }
    return mask(line);
  });
  // Inline spans pair whole, equal-length backtick runs within a paragraph.
  return lines.join("\n").replace(/(?<!`)(`+)(?!`)(?:(?!\n[ \t]*\r?\n)[\s\S])*?(?<!`)\1(?!`)/g, mask);
}
