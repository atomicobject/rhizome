import { describe, expect, it } from "vitest";

import { noteExcerptParts } from "./searchExcerpt";

const text = (snippet: string) =>
  noteExcerptParts(snippet)
    .map((part) => (part.code ? `[${part.text}]` : part.text))
    .join("");

describe("noteExcerptParts", () => {
  it("joins list items into one line and marks inline code", () => {
    expect(
      text(
        "- For typed roots, use exactly one of:\n  - `path`\n  - `find`\n- `first` defaults to `20`.",
      ),
    ).toBe("For typed roots, use exactly one of: [path] · [find] · [first] defaults to [20].");
  });

  it("ends a snippet the server cut mid-word at the last whole word", () => {
    const cut = `${"word ".repeat(47)}document/code/test p`;
    expect(cut).toHaveLength(255);
    expect(text(cut)).toBe(`${"word ".repeat(47)}document/code/test…`);
  });

  it("keeps a short snippet whole and drops an unpaired backtick", () => {
    expect(text("Soft\nwrapped **bold** text with `open")).toBe("Soft wrapped bold text with open");
  });
});
