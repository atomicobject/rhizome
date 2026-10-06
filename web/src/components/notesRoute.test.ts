import { describe, expect, it } from "vitest";

import type { OntologyInterfaceSummary, OntologyTypeSummary } from "../api/types";
import {
  buildNotesLocation,
  buildNotesPath,
  normalizeNotePath,
  parseNotesLocation,
  parseNotesRoute,
  resolveTypeNameFromSlug,
  splitNoteTarget,
} from "./notesRoute";

const makeType = (name: string): OntologyTypeSummary => ({
  name,
  label: name,
  color: "#000",
  description: "",
  count: 0,
  issueCount: 0,
  startingNotes: [],
});

const makeIface = (name: string): OntologyInterfaceSummary => ({
  name,
  label: name,
  description: "",
  count: 0,
  issueCount: 0,
  implementors: [],
});

describe("parseNotesRoute", () => {
  it("maps root and pseudo slugs", () => {
    expect(parseNotesRoute("/notes")).toEqual({ kind: "all" });
    expect(parseNotesRoute("/notes/issues")).toEqual({ kind: "issues" });
    expect(parseNotesRoute("/notes/modified")).toEqual({ kind: "modified" });
  });

  it("treats unknown suffixes as type slugs", () => {
    expect(parseNotesRoute("/notes/spec-like")).toEqual({
      kind: "type",
      slug: "spec-like",
    });
  });

  it("falls back safely when a group link has invalid URL encoding", () => {
    expect(parseNotesRoute("/notes/group/%E0%A4")).toEqual({ kind: "all" });
  });
});

describe("buildNotesPath", () => {
  it("formats type names into slugs", () => {
    expect(buildNotesPath({ kind: "type", typeName: "SpecLike" })).toBe("/notes/spec-like");
    expect(buildNotesPath({ kind: "all" })).toBe("/notes");
  });
});

describe("parseNotesLocation", () => {
  it("parses the selection, note, and fragment", () => {
    expect(parseNotesLocation("/notes/spec-like", "?note=docs%2Fspec.md", "#section%201")).toEqual({
      selection: { kind: "type", slug: "spec-like" },
      note: "docs/spec.md",
      query: null,
      fragment: "section 1",
      view: null,
    });
  });

  it("parses standalone views without a note or fragment", () => {
    expect(parseNotesLocation("/notes", "?view=planning.backlog", "#ignored")).toEqual({
      selection: { kind: "all" },
      note: null,
      query: null,
      fragment: null,
      view: "planning.backlog",
    });
  });

  it("parses a note and its fragment alongside the Home view", () => {
    expect(parseNotesLocation("/notes", "?view=planning.backlog&note=a.md", "#section")).toEqual({
      selection: { kind: "all" },
      note: "a.md",
      query: null,
      fragment: "section",
      view: "planning.backlog",
    });
  });

  it.each([
    ["?view=v1", "#%5Estory-001", null, null],
    ["", "", null, null],
    ["?note=notes%2Ftask-flow.md", "", "notes/task-flow.md", null],
    ["?note=notes%2Fa.md", "#", "notes/a.md", null],
    ["?note=notes%2Fa.md", "#^story-001", "notes/a.md", "^story-001"],
    ["?note=notes%2Fa.md", "#%5Estory-001", "notes/a.md", "^story-001"],
    ["?note=notes%2Fa.md", "#Search%20Rewrite", "notes/a.md", "Search Rewrite"],
    ["?note=notes%2Fa.md", "#%E0story", "notes/a.md", "%E0story"],
    ["?note=notes%2Fa.md", "#20%2520off", "notes/a.md", "20%20off"],
    [
      "?note=notes%2Fa.md",
      "#Search%20Rewrite%3A%20Phase%201",
      "notes/a.md",
      "Search Rewrite: Phase 1",
    ],
  ])("reads %s%s as note %s with fragment %s", (search, hash, note, fragment) => {
    expect(parseNotesLocation("/notes", search, hash)).toMatchObject({ note, fragment });
  });

  it("tolerates malformed fragment escapes", () => {
    expect(parseNotesLocation("/notes", "?note=docs%2Fa.md", "#bad%2")).toEqual({
      selection: { kind: "all" },
      note: "docs/a.md",
      query: null,
      fragment: "bad%2",
      view: null,
    });
  });

  it("parses a search query and its effective filters", () => {
    expect(
      parseNotesLocation(
        "/notes",
        "?search=architecture%20review&scope=code&folder=src%2Fsearch",
        "",
      ),
    ).toEqual({
      selection: { kind: "all" },
      note: null,
      query: null,
      fragment: null,
      view: null,
      search: "architecture review",
      searchFilters: { scope: "code", noteType: null, folder: "src/search" },
    });
  });

  it("parses a stable search tab id", () => {
    expect(
      parseNotesLocation("/notes", "?search=architecture&searchTab=search-tab%3A7", ""),
    ).toMatchObject({ search: "architecture", searchTabID: "search-tab:7" });
  });

  it("parses a scoped selected problem alongside note context", () => {
    expect(
      parseNotesLocation(
        "/notes/issues",
        "?note=docs%2Fspec.md&issueScopeKind=note&issueScopeKey=docs%2Fspec.md&issue=issue-1",
        "#issue:field=status",
      ),
    ).toMatchObject({
      selection: { kind: "issues" },
      note: "docs/spec.md",
      fragment: "issue:field=status",
      issueScope: { kind: "note", key: "docs/spec.md" },
      issueKey: "issue-1",
    });
  });

  it("round-trips a folder listing, which is a search with an empty query", () => {
    const url = buildNotesLocation({
      selection: { kind: "all" },
      search: "",
      folder: "Notes/",
      searchTabID: "search-tab:3",
    });

    expect(url).toBe("/notes?search=&folder=Notes&searchTab=search-tab%3A3");
    const [, query] = url.split("?");
    expect(parseNotesLocation("/notes", `?${query}`, "")).toMatchObject({
      search: "",
      searchFilters: { scope: "all", noteType: null, folder: "Notes" },
      searchTabID: "search-tab:3",
    });
  });

  it("round-trips a query with a folder", () => {
    const url = buildNotesLocation({ selection: { kind: "all" }, search: "plan", folder: "Notes" });

    expect(url).toBe("/notes?search=plan&folder=Notes");
    expect(parseNotesLocation("/notes", url.slice("/notes".length), "")).toMatchObject({
      search: "plan",
      searchFilters: { folder: "Notes" },
    });
  });

  it("ignores an empty search without a folder", () => {
    expect(buildNotesLocation({ selection: { kind: "all" }, search: " " })).toBe("/notes");
    expect(parseNotesLocation("/notes", "?search=", "").search).toBeUndefined();
  });

  it("gives an explicit note URL precedence over search parameters", () => {
    const location = parseNotesLocation("/notes", "?note=a.md&search=ignored", "");
    expect(location.note).toBe("a.md");
    expect(location.search).toBeUndefined();
  });
});

describe("buildNotesLocation", () => {
  it("keeps scoped problem selection when opening exact note context", () => {
    expect(
      buildNotesLocation({
        selection: { kind: "issues" },
        note: "docs/spec.md",
        fragment: "issue:field=status",
        issueScope: { kind: "note", key: "docs/spec.md" },
        issueKey: "issue-1",
      }),
    ).toBe(
      "/notes/issues?issueScopeKind=note&issueScopeKey=docs%2Fspec.md&issue=issue-1&note=docs%2Fspec.md#issue%3Afield%3Dstatus",
    );
  });
  it("preserves explicit HTML note paths and fragments in shareable URLs", () => {
    const location = buildNotesLocation({
      selection: { kind: "all" },
      note: normalizeNotePath("reports/Status.HTML"),
      fragment: "results",
    });

    expect(location).toBe("/notes?note=reports%2FStatus.HTML#results");
    expect(parseNotesLocation("/notes", "?note=reports%2FStatus.HTML", "#results")).toEqual({
      selection: { kind: "all" },
      note: "reports/Status.HTML",
      query: null,
      fragment: "results",
      view: null,
    });
  });

  it("round-trips an encoded note target", () => {
    const location = buildNotesLocation({
      selection: { kind: "type", slug: "spec-like" },
      note: "docs/spec.md",
      fragment: "section 1",
    });

    expect(location).toBe("/notes/spec-like?note=docs%2Fspec.md#section%201");
    expect(parseNotesLocation("/notes/spec-like", "?note=docs%2Fspec.md", "#section%201")).toEqual({
      selection: { kind: "type", slug: "spec-like" },
      note: "docs/spec.md",
      query: null,
      fragment: "section 1",
      view: null,
    });
  });

  it("accepts canonical type names and retains the Home view alongside a note", () => {
    expect(
      buildNotesLocation({
        selection: { kind: "type", typeName: "SpecLike" },
        note: "docs/spec.md",
        fragment: "section",
        view: "planning.backlog",
      }),
    ).toBe("/notes/spec-like?view=planning.backlog&note=docs%2Fspec.md#section");
  });

  it("omits empty query and hash suffixes", () => {
    expect(buildNotesLocation({ selection: { kind: "all" }, note: "" })).toBe("/notes");
  });

  it.each([
    ["", "/notes?note=notes%2Fa.md"],
    ["#", "/notes?note=notes%2Fa.md"],
    ["^story-001", "/notes?note=notes%2Fa.md#%5Estory-001"],
    ["20%20off", "/notes?note=notes%2Fa.md#20%2520off"],
    ["Search Rewrite: Phase 1", "/notes?note=notes%2Fa.md#Search%20Rewrite%3A%20Phase%201"],
  ])("writes note fragment %j as %s", (fragment, expected) => {
    expect(buildNotesLocation({ selection: { kind: "all" }, note: "notes/a.md", fragment })).toBe(
      expected,
    );
  });

  it("builds a filtered search URL", () => {
    const location = buildNotesLocation({
      selection: { kind: "all" },
      search: "architecture review",
      scope: "code",
      folder: "src/search/",
    });

    expect(location).toBe("/notes?search=architecture+review&scope=code&folder=src%2Fsearch");
  });

  it("includes a stable search tab id in search URLs", () => {
    expect(
      buildNotesLocation({
        selection: { kind: "all" },
        search: "architecture",
        searchTabID: "search-tab:7",
      }),
    ).toBe("/notes?search=architecture&searchTab=search-tab%3A7");
  });

  it("keeps authored document query separate from application routing params", () => {
    const location = buildNotesLocation({
      selection: { kind: "all" },
      view: "planning.backlog",
      note: "reports/status.html",
      query: "filter=active&sort=desc",
      fragment: "results",
    });

    expect(location).toBe(
      "/notes?view=planning.backlog&note=reports%2Fstatus.html&noteQuery=filter%3Dactive%26sort%3Ddesc#results",
    );
    expect(
      parseNotesLocation("/notes", new URL(location, "https://example.test").search, "#results"),
    ).toEqual({
      selection: { kind: "all" },
      note: "reports/status.html",
      query: "filter=active&sort=desc",
      fragment: "results",
      view: "planning.backlog",
    });
  });
});

describe("normalizeNotePath", () => {
  it("preserves explicit note extensions while retaining extensionless Markdown compatibility", () => {
    expect(normalizeNotePath("reports/status.html")).toBe("reports/status.html");
    expect(normalizeNotePath("reports/status.HTM")).toBe("reports/status.HTM");
    expect(normalizeNotePath("reports/status.MD")).toBe("reports/status.MD");
    expect(normalizeNotePath("notes/README.MD")).toBe("notes/README.MD");
    expect(normalizeNotePath("reports/status.future")).toBe("reports/status.future.md");
    expect(normalizeNotePath("reports/status")).toBe("reports/status.md");
    expect(normalizeNotePath("decisions/Design v2.0")).toBe("decisions/Design v2.0.md");
    expect(normalizeNotePath("decisions/Architecture.Draft")).toBe(
      "decisions/Architecture.Draft.md",
    );
    expect(normalizeNotePath("notes/ASP.NET")).toBe("notes/ASP.NET.md");
  });
});

describe("splitNoteTarget", () => {
  it("decodes a literal percent escape exactly once at the browser boundary", () => {
    const target = splitNoteTarget("docs/spec.md#section%201");

    const url = new URL(
      buildNotesLocation({
        selection: { kind: "all" },
        note: target.path,
        fragment: target.fragment,
      }),
      "https://example.test",
    );

    expect(url.hash).toBe("#section%25201");
    expect(parseNotesLocation(url.pathname, url.search, url.hash).fragment).toBe("section%201");
  });
  it("splits an internal target without decoding literal percent escapes", () => {
    expect(splitNoteTarget("docs/spec.md#section%201")).toEqual({
      path: "docs/spec.md",
      query: null,
      fragment: "section%201",
    });
  });

  it("keeps a target without a fragment intact", () => {
    expect(splitNoteTarget("docs/spec.md")).toEqual({
      path: "docs/spec.md",
      query: null,
      fragment: null,
    });
  });

  it("splits authored query and fragment without treating either as file identity", () => {
    expect(splitNoteTarget("reports/status.html?filter=active&sort=desc#results")).toEqual({
      path: "reports/status.html",
      query: "filter=active&sort=desc",
      fragment: "results",
    });

    expect(splitNoteTarget("reports/status.html?filter=active")).toEqual({
      path: "reports/status.html",
      query: "filter=active",
      fragment: null,
    });
  });
});

describe("resolveTypeNameFromSlug", () => {
  it("matches concrete types first", () => {
    const types = [makeType("ProcessSpec")];
    expect(resolveTypeNameFromSlug("process-spec", types)).toBe("ProcessSpec");
  });

  it("falls through to interfaces when no type matches", () => {
    const types = [makeType("ProcessSpec")];
    const interfaces = [makeIface("SpecLike")];
    expect(resolveTypeNameFromSlug("spec-like", types, interfaces)).toBe("SpecLike");
  });

  it("returns null for unknown slugs", () => {
    expect(resolveTypeNameFromSlug("mystery", [], [])).toBeNull();
  });
});

describe("mounted presentations", () => {
  it("roundtrips a display group and keeps its presentation distinct from standalone views", () => {
    const href = buildNotesLocation({
      selection: { kind: "group", group: "Agentic Engineering" },
      presentation: "engineering.dashboard",
    });

    const url = new URL(href, "http://localhost");
    expect(parseNotesLocation(url.pathname, url.search, url.hash)).toMatchObject({
      selection: { kind: "group", group: "Agentic Engineering" },
      presentation: "engineering.dashboard",
      view: null,
    });
  });
  it("roundtrips canonical focused node identity and custom presentation", () => {
    const href = buildNotesLocation({
      selection: { kind: "type", typeName: "Spec" },
      note: "specs/demo.md",
      fragment: "Details",
      nodeId: "node-2",
      nodeKind: "HEADING",
      structural: "fingerprint",
      presentation: "spec.detail",
    });

    const url = new URL(href, "http://localhost");
    expect(parseNotesLocation(url.pathname, url.search, url.hash)).toMatchObject({
      note: "specs/demo.md",
      fragment: "Details",
      nodeId: "node-2",
      nodeKind: "HEADING",
      structural: "fingerprint",
      presentation: "spec.detail",
    });
  });
});
