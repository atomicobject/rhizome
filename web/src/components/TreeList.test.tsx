import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { TreeEntry } from "../api/types";
import { TreeList } from "./TreeList";

const docs: TreeEntry = { name: "docs", path: "docs", kind: "dir", hasChildren: true };

const notes: TreeEntry = { name: "notes", path: "notes", kind: "dir", hasChildren: true };

const readme: TreeEntry = {
  name: "readme.md",
  path: "docs/readme.md",
  kind: "note",
  hasChildren: false,
};

describe("TreeList", () => {
  it("exposes folder expansion and the selected file without reading icons aloud", () => {
    render(
      <TreeList
        entries={[docs, notes]}
        expanded={{ docs: [readme] }}
        selectedPath={readme.path}
        onSelect={() => {}}
        onToggle={() => {}}
      />,
    );

    expect(screen.getByRole("button", { name: "docs" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("button", { name: "notes" })).toHaveAttribute("aria-expanded", "false");

    const file = screen.getByRole("button", { name: "readme.md" });
    expect(file).toHaveAttribute("aria-current", "page");
    expect(file).not.toHaveAttribute("aria-expanded");
    expect(screen.getByRole("button", { name: "docs" })).not.toHaveAttribute("aria-current");
  });
});
