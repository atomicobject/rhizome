import { describe, expect, it } from "vitest";

import type { NodeWorkspace, ViewCatalog, ViewPanel, ViewTarget } from "../api/types";
import { activePanel } from "./activePanel";
import type { Tab } from "./useNoteTabs";

const target = (kind: ViewTarget["kind"], name: string, panel: ViewPanel): ViewTarget => ({
  kind,
  name,
  panel,
  choices: [],
  defaultChoiceId: "",
});

const kb: ViewPanel = { kind: "view", viewId: "kb.panel", experienceKey: "exp_kb" };

const catalog: ViewCatalog = {
  views: [],
  targets: [
    target("node", "Assertion", kb),
    target("node", "Spec", { kind: "owner" }),
    target("node", "Criterion", { kind: "owner" }),
    target("group", "Knowledge", kb),
    target("standalone", "kb.home", kb),
    target("workspace", "", { kind: "none" }),
  ],
};

const workspace = (kind: string, resolvedType: string, noteType: string): NodeWorkspace =>
  // SAFETY: activePanel reads only the node descriptor.
  ({
    node: {
      ref: { notePath: "a.md", kind },
      resolvedType,
      noteType,
      notePath: "a.md",
      locator: kind,
    },
  }) as NodeWorkspace;

const note: Tab = { id: "note:a.md", kind: "note", path: "a.md", dirty: false };

const resolve = (tab: Tab, nodeWorkspace?: NodeWorkspace) =>
  activePanel({
    tab,
    catalog,
    homeSelection: { kind: "all" },
    homeType: null,
    summary: null,
    workspace: nodeWorkspace,
  });

describe("activePanel", () => {
  it("shows the server's panel for a note's type, with the node as its context", () => {
    expect(resolve(note, workspace("NOTE", "Assertion", "Assertion"))).toEqual({
      context: {
        kind: "node",
        type: "Assertion",
        ref: { notePath: "a.md", kind: "NOTE" },
      },
      panel: kb,
    });
  });

  it("follows an owner descriptor to the owning note's panel, or Folder for a note", () => {
    expect(resolve(note, workspace("EMBEDDED", "Criterion", "Assertion"))?.panel).toEqual(kb);
    expect(resolve(note, workspace("NOTE", "Spec", "Spec"))?.panel).toEqual({ kind: "folder" });
    expect(resolve(note, workspace("EMBEDDED", "Criterion", "Spec"))?.panel).toEqual({
      kind: "folder",
    });
    expect(resolve(note, workspace("NOTE", "Untargeted", ""))?.panel).toEqual({ kind: "folder" });
  });

  it("names a standalone view in its context and shows no panel for search", () => {
    expect(resolve({ id: "view:kb.home", kind: "view", viewId: "kb.home", title: "" })).toEqual({
      context: { kind: "standalone", view: "kb.home" },
      panel: kb,
    });
    expect(
      resolve({
        id: "search-tab:1",
        kind: "search",
        query: "x",
        filters: { scope: "all", noteType: null, folder: null },
        scrollTop: 0,
      })?.panel,
    ).toEqual({ kind: "none" });
  });

  it("waits for the note's node and the catalog", () => {
    expect(resolve(note)).toBeNull();
    expect(
      activePanel({
        tab: note,
        catalog: undefined,
        homeSelection: { kind: "all" },
        homeType: null,
        summary: null,
        workspace: workspace("NOTE", "Assertion", "Assertion"),
      }),
    ).toBeNull();
  });

  it("uses the home tab's group", () => {
    expect(
      activePanel({
        tab: { id: "home", kind: "home" },
        catalog,
        homeSelection: { kind: "group", group: "Knowledge" },
        homeType: null,
        summary: null,
        workspace: null,
      }),
    ).toEqual({ context: { kind: "group", group: "Knowledge" }, panel: kb });
  });
});
