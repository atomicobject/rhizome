import type { Parent, Root, Text } from "mdast";
import { decodeString } from "micromark-util-decode-string";
import type { Point } from "unist";
import type { Plugin } from "unified";
import { SKIP, visit } from "unist-util-visit";

interface Wikilink extends Parent {
  type: "rzWikilink";
  target: string;
  label: string;
  children: Text[];
}

declare module "mdast" {
  interface PhrasingContentMap {
    rzWikilink: Wikilink;
  }

  interface RootContentMap {
    rzWikilink: Wikilink;
  }
}

function point(start: Point, raw: string, index: number): Point {
  const lines = raw.slice(0, index).split(/\r\n|\r|\n/);

  return {
    line: start.line + lines.length - 1,
    column: lines.length === 1 ? start.column + index : (lines.at(-1)?.length ?? 0) + 1,
    offset: (start.offset ?? 0) + index,
  };
}

function escaped(source: string, at: number) {
  let slashes = 0;

  for (let i = at - 1; i >= 0 && source[i] === "\\"; i--) slashes++;

  return slashes % 2 !== 0;
}

function textPrefix(node: Text, source: string, at: number) {
  const start = node.position?.start.offset;

  if (start === undefined) return undefined;

  const prefix = decodeString(
    source.slice(start, at).replace(/(\r\n|\r|\n)[ \t]*(?:>[ \t]*)*/g, "$1"),
  );

  return node.value.startsWith(prefix) ? prefix : undefined;
}

// Match authored syntax after CommonMark establishes links, code and HTML boundaries.
export const remarkWikilinks: Plugin<[], Root> = () => (tree, file) => {
  const source = String(file);
  visit(tree, (node) => {
    if (node.type === "link" || node.type === "linkReference") return SKIP;

    if (
      node.type !== "paragraph" &&
      node.type !== "heading" &&
      node.type !== "tableCell" &&
      node.type !== "emphasis" &&
      node.type !== "strong" &&
      node.type !== "delete"
    )
      return;

    if (!node.position) return;
    const start = node.position.start.offset;
    const end = node.position.end.offset;

    if (start === undefined || end === undefined) return;
    const raw = source.slice(start, end);

    for (const match of raw.matchAll(/!?\[\[((?:\\[^\r\n]|[^[\]\r\n])+?)\]\]/g)) {
      let from = start + match.index;
      const to = from + match[0].length;

      if (match[0].startsWith("!") && escaped(source, from)) from++;

      if (escaped(source, from)) continue;

      const first = node.children.findIndex(
        (child) =>
          child.type === "text" &&
          child.position?.start.offset !== undefined &&
          child.position.end.offset !== undefined &&
          child.position.start.offset <= from &&
          from < child.position.end.offset,
      );

      const last = node.children.findIndex(
        (child) =>
          child.type === "text" &&
          child.position?.start.offset !== undefined &&
          child.position.end.offset !== undefined &&
          child.position.start.offset < to &&
          to <= child.position.end.offset,
      );

      if (first < 0 || last < first) continue;
      const before = node.children[first];
      const after = node.children[last];

      if (before.type !== "text" || after.type !== "text" || !before.position || !after.position)
        continue;
      // CommonMark may split a filename like __init__.py into emphasis siblings.
      // Existing links, HTML and media inside the range keep their own meaning.
      let blocked = false;

      for (const child of node.children.slice(first, last + 1))
        visit(child, (part) => {
          if (["link", "linkReference", "html", "image", "imageReference"].includes(part.type))
            blocked = true;
        });

      if (blocked) continue;
      const prefix = textPrefix(before, source, from);
      const consumed = textPrefix(after, source, to);

      if (prefix === undefined || consumed === undefined) continue;
      const [head, ...aliases] = decodeString(match[1]).split("|");
      const target = head.trim();

      if (!target) continue;
      const children: (Text | Wikilink)[] = [];

      if (prefix)
        children.push({
          type: "text",
          value: prefix,
          position: {
            start: before.position.start,
            end: point(node.position.start, raw, from - start),
          },
        });
      children.push({
        type: "rzWikilink",
        target,
        label: aliases.join("|").trim() || target,
        children: [],
        position: {
          start: point(node.position.start, raw, from - start),
          end: point(node.position.start, raw, to - start),
        },
      });
      const suffix = after.value.slice(consumed.length);

      if (suffix)
        children.push({
          type: "text",
          value: suffix,
          position: {
            start: point(node.position.start, raw, to - start),
            end: after.position.end,
          },
        });
      node.children.splice(first, last - first + 1, ...children);
    }
  });
};
