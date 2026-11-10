import { unified } from "unified";
import remarkParse from "remark-parse";
import remarkGfm from "remark-gfm";
import type { Nodes } from "mdast";

const parser = unified().use(remarkParse).use(remarkGfm);

export type MarkdownTaskCounts = { checked: number; total: number };

/** Recorded task items, using the same GFM grammar as Markdown rendering. */
export function markdownChecklist(source: string): MarkdownTaskCounts {
  const pending: Nodes[] = [parser.parse(source)];
  let checked = 0;
  let total = 0;

  while (pending.length) {
    const node = pending.pop()!;

    if (node.type === "listItem" && (node.checked === true || node.checked === false)) {
      total++;

      if (node.checked) checked++;
    }

    if ("children" in node) for (const child of node.children) pending.push(child);
  }

  return { checked, total };
}
