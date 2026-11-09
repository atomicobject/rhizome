import { fireEvent, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import type { FolderOverview, GraphNode } from "../api/types";
import { withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { FolderPanel, folderRows } from "./FolderPanel";
import { buildFolderTree } from "./folderTree";

const http = withFakeFetch();

const note = (notePath: string, label: string): GraphNode => ({
  id: `note:${notePath}`,
  kind: "note",
  label,
  notePath,
});

const overview: FolderOverview = {
  files: [
    note("docs/specs/beta.md", "Beta spec"),
    note("docs/specs/alpha.md", "Alpha spec"),
    note("docs/specs/drafts/early.md", "Early draft"),
    note("docs/guide.md", "Guide"),
    note("readme.md", "Readme"),
    { id: "code:docs/specs/gen.go", kind: "code", label: "gen.go", path: "docs/specs/gen.go" },
  ],
  links: [],
  nestedTypes: [],
  truncated: false,
};

beforeEach(() => {
  window.history.replaceState({}, "", "/notes/types/Spec");
  http.json("GET", "/api/v1/graphs/folders", overview);
});

describe("folder rows", () => {
  const tree = buildFolderTree(overview);

  it("lists the note's ancestors down to its folder, then that folder's subfolders and notes", () => {
    expect(
      folderRows(tree, "docs/specs/beta.md")?.map((row) => [
        row.kind,
        row.name,
        row.depth,
        row.current,
      ]),
    ).toEqual([
      ["folder", "docs", 0, false],
      ["folder", "specs", 1, true],
      ["folder", "drafts", 2, false],
      ["note", "Alpha spec", 2, false],
      ["note", "Beta spec", 2, true],
    ]);
  });

  it("lists a top-level note's siblings without ancestors", () => {
    expect(folderRows(tree, "readme.md")?.map((row) => row.name)).toEqual(["docs", "Readme"]);
  });

  it("gives nothing for a folder the graph does not hold", () => {
    expect(folderRows(tree, "elsewhere/note.md")).toBeNull();
  });
});

describe("the Folder panel", () => {
  it("opens a note in Notes, keeping the page under it, and a folder in Explorer", async () => {
    render(<FolderPanel notePath="docs/specs/beta.md" />);
    const list = await screen.findByRole("navigation", { name: "Folder contents" });

    expect(within(list).getByRole("button", { name: "Beta spec" })).toHaveAttribute(
      "aria-current",
      "page",
    );

    fireEvent.click(within(list).getByRole("button", { name: "Alpha spec" }));
    expect(window.location.pathname).toBe("/notes/types/Spec");
    expect(new URLSearchParams(window.location.search).get("note")).toBe("docs/specs/alpha.md");

    fireEvent.click(within(list).getByRole("button", { name: "specs" }));
    expect(window.location.pathname).toBe("/explorer");
    expect(new URLSearchParams(window.location.search).get("folder")).toBe("docs/specs");
  });

  it("says when the note's folder is not in the graph", async () => {
    render(<FolderPanel notePath="elsewhere/note.md" />);

    expect(
      await screen.findByText("This note’s folder is not in the file graph yet."),
    ).toBeVisible();
  });
});
