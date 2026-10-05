import { styleTags, tags } from "@lezer/highlight";
import type { MarkdownConfig } from "@lezer/markdown";

const FENCE = /^---\s*$/;

// YAML frontmatter is not CommonMark, so without this block parser the closing
// `---` turns the last metadata line into a setext heading and the fences into
// thematic breaks. The block only exists at document start.
export const frontmatterBlock: MarkdownConfig = {
  defineNodes: [
    { name: "Frontmatter", block: true },
    { name: "FrontmatterMark" },
    { name: "FrontmatterContent" },
  ],
  props: [
    styleTags({
      Frontmatter: tags.monospace,
      FrontmatterMark: tags.processingInstruction,
      FrontmatterContent: tags.meta,
    }),
  ],
  parseBlock: [
    {
      name: "Frontmatter",
      before: "HorizontalRule",
      parse(cx, line) {
        if (cx.lineStart !== 0 || !FENCE.test(line.text)) return false;
        const start = cx.lineStart;
        const marks = [cx.elt("FrontmatterMark", start, start + line.text.length)];
        let contentFrom = -1;
        let contentTo = -1;

        while (cx.nextLine()) {
          if (FENCE.test(line.text)) {
            const closeFrom = cx.lineStart;
            marks.push(cx.elt("FrontmatterMark", closeFrom, closeFrom + line.text.length));
            const children = [marks[0]];

            if (contentFrom >= 0)
              children.push(cx.elt("FrontmatterContent", contentFrom, contentTo));
            children.push(marks[1]);
            cx.addElement(cx.elt("Frontmatter", start, closeFrom + line.text.length, children));
            cx.nextLine();

            return true;
          }

          if (contentFrom < 0) contentFrom = cx.lineStart;
          contentTo = cx.lineStart + line.text.length;
        }

        return false;
      },
    },
  ],
};
