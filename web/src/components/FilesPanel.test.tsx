import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { FolderOverview, GraphNode } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { FilesPanel, fileTreeRows, noteFolders } from "./FilesPanel";
import { buildFolderTree } from "./folderTree";

const http = withFakeFetch();

const note = (notePath: string): GraphNode => ({
  id: `note:${notePath}`,
  kind: "note",
  label: notePath,
  notePath,
});

const overview: FolderOverview = {
  files: [
    note("docs/specs/beta.md"),
    note("docs/specs/alpha.md"),
    note("docs/specs/drafts/early.md"),
    note("docs/efforts/plan.html"),
    note("readme.md"),
    { id: "code:src/main.go", kind: "code", label: "main.go", path: "src/main.go" },
  ],
  links: [],
  nestedTypes: [],
  truncated: false,
};

beforeEach(() => {
  window.localStorage.clear();
  window.history.replaceState({}, "", "/notes/types/Spec");
  http.json("GET", "/api/v1/graphs/folders", overview);
});

describe("file tree rows", () => {
  const tree = buildFolderTree(overview);

  const outline = (open: string[], current: string | null = null) =>
    fileTreeRows(tree, (folder) => open.includes(folder), current).map(
      (row) => `${"  ".repeat(row.depth)}${row.name}${row.current ? " *" : ""}`,
    );

  it("lists folders holding notes before notes, leaving out code-only folders", () => {
    expect(outline([])).toEqual(["docs", "readme.md"]);
  });

  it("lists an open folder's subfolders and notes beneath it, by name", () => {
    expect(outline(["docs", "docs/specs"], "docs/specs/beta.md")).toEqual([
      "docs",
      "  efforts",
      "  specs",
      "    drafts",
      "    alpha.md",
      "    beta.md *",
      "readme.md",
    ]);
  });

  it("names the folders that hold a note", () => {
    expect(noteFolders("docs/specs/drafts/early.md")).toEqual([
      "docs",
      "docs/specs",
      "docs/specs/drafts",
    ]);
    expect(noteFolders("readme.md")).toEqual([]);
  });
});

describe("the Files panel", () => {
  it("reveals and selects the active note, and opens a note on click", async () => {
    const onOpenNote = vi.fn();
    render(<FilesPanel notePath="docs/specs/beta.md" onOpenNote={onOpenNote} />);
    const tree = await screen.findByRole("navigation", { name: "Files" });

    expect(within(tree).getByRole("button", { name: "beta.md" })).toHaveAttribute(
      "aria-current",
      "page",
    );

    fireEvent.click(within(tree).getByRole("button", { name: "alpha.md" }));
    expect(onOpenNote).toHaveBeenCalledWith("docs/specs/alpha.md");
  });

  it("expands and collapses folders, and remembers them", async () => {
    const first = render(<FilesPanel notePath={null} />);
    const tree = await screen.findByRole("navigation", { name: "Files" });
    const docs = within(tree).getByRole("button", { name: "docs" });

    expect(docs).toHaveAttribute("aria-expanded", "false");
    expect(within(tree).queryByRole("button", { name: "efforts" })).toBeNull();

    fireEvent.click(docs);
    expect(docs).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(within(tree).getByRole("button", { name: "efforts" }));
    expect(within(tree).getByRole("button", { name: "plan.html" })).toBeVisible();
    first.unmount();

    render(<FilesPanel notePath={null} />);
    expect(await screen.findByRole("button", { name: "plan.html" })).toBeVisible();
  });

  it("toggles the focused folder with the arrow keys, and never a folder from a note", async () => {
    render(<FilesPanel notePath={null} />);
    const tree = await screen.findByRole("navigation", { name: "Files" });
    const docs = within(tree).getByRole("button", { name: "docs" });
    fireEvent.click(docs);
    const efforts = within(tree).getByRole("button", { name: "efforts" });
    const specs = within(tree).getByRole("button", { name: "specs" });

    fireEvent.keyDown(specs, { key: "ArrowRight" });
    expect(specs).toHaveAttribute("aria-expanded", "true");
    expect(efforts).toHaveAttribute("aria-expanded", "false");

    fireEvent.keyDown(within(tree).getByRole("button", { name: "readme.md" }), {
      key: "ArrowLeft",
    });
    expect(docs).toHaveAttribute("aria-expanded", "true");

    fireEvent.keyDown(specs, { key: "ArrowLeft" });
    expect(specs).toHaveAttribute("aria-expanded", "false");
  });

  it("opens a note in Notes by URL, keeping the page under it, without a workspace handler", async () => {
    render(<FilesPanel notePath={null} />);
    fireEvent.click(await screen.findByRole("button", { name: "readme.md" }));

    expect(window.location.pathname).toBe("/notes/types/Spec");
    expect(new URLSearchParams(window.location.search).get("note")).toBe("readme.md");
  });
});
