import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { WorkspaceSearchResponse } from "../api/types";
import { queryKeys } from "../api/queryKeys";
import { jsonReply, withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { SearchWorkspace } from "./SearchWorkspace";
import type { SearchTab } from "./useNoteTabs";

const tab: SearchTab = {
  id: "search-tab:1",
  kind: "search",
  query: "embedded identifiers",
  filters: { scope: "all", noteType: null, folder: null },
  scrollTop: 0,
};

describe("SearchWorkspace", () => {
  const http = withFakeFetch();

  beforeEach(() => {
    window.history.replaceState({}, "", "/notes?search=embedded+identifiers");
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function renderSearch(response?: WorkspaceSearchResponse | null, query = tab.query) {
    if (response !== null) {
      http.json(
        "GET",
        "/api/v1/search",
        response ?? {
          count: 1,
          total: 1,
          matches: [
            {
              type: "note",
              path: "docs/spec.md",
              title: "Embedded identifiers",
              snippetStatus: "available",
              snippet: "Durable embedded identifiers keep links stable.",
              nodeRef: {
                notePath: "docs/spec.md",
                fragment: "requirements-1234",
                kind: "structural",
              },
            },
          ],
        },
      );
    }

    const onRefineSearch = vi.fn();
    const onOpenNote = vi.fn();
    const onScrollPosition = vi.fn();

    const rendered = render(
      <SearchWorkspace
        tab={{ ...tab, query }}
        active
        types={[
          { name: "Spec", label: "Spec", count: 1, role: "note" },
          {
            name: "AcceptanceCriterion",
            label: "Acceptance criterion",
            count: 4,
            role: "embedded",
          },
        ]}
        onRefineSearch={onRefineSearch}
        onOpenNote={onOpenNote}
        onScrollPosition={onScrollPosition}
      />,
    );

    return { ...rendered, onRefineSearch, onOpenNote, onScrollPosition };
  }

  it.each(["_FallbackNote", "_FallbackSection"])(
    "hides internal search type %s",
    async (noteType) => {
      renderSearch({
        count: 1,
        total: 1,
        matches: [{ type: "note", noteType, title: "Plain section", path: "plain.md" }],
      });
      expect(await screen.findByRole("button", { name: "Plain section" })).toBeVisible();
      expect(screen.queryByText(new RegExp(noteType))).toBeNull();
    },
  );

  it("renders genuine excerpts and opens canonical note targets", async () => {
    const { onOpenNote } = renderSearch();

    expect(await screen.findByRole("heading", { name: "Search results" })).toBeVisible();
    expect(screen.queryByRole("option", { name: "Acceptance criterion" })).toBeNull();
    expect(screen.getByText("embedded identifiers")).toBeVisible();
    await screen.findByRole("button", { name: "Embedded identifiers" });
    expect(
      screen.getByText(
        (_, element) =>
          element?.tagName === "P" &&
          element.textContent === "Durable embedded identifiers keep links stable.",
      ),
    ).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Embedded identifiers" }), {
      metaKey: true,
    });
    expect(onOpenNote).toHaveBeenCalledWith("docs/spec.md#requirements-1234", "beside");
  });

  it("previews note results at their canonical targets", async () => {
    http.json("GET", "/api/v1/nodes/preview", {
      ref: "docs/spec.md#requirements-1234",
      path: "docs/spec.md",
      title: "Embedded identifiers preview",
      format: "markdown",
      fragmentResolved: true,
      fields: [],
      hasIssues: false,
    });
    renderSearch();
    const result = await screen.findByRole("button", { name: "Embedded identifiers" });

    fireEvent.focus(result);

    await waitFor(() =>
      expect(http.requests("GET", "/api/v1/nodes/preview")[0]?.query.get("ref")).toBe(
        "docs/spec.md#requirements-1234",
      ),
    );
    expect(
      await screen.findByRole("region", { name: "Preview of Embedded identifiers preview" }),
    ).toBeVisible();
  });

  it.each([
    {
      query: "  embedded\tidentifiers  ",
      text: "Embedded identifiers stay EMBEDDED.",
      marks: ["Embedded", "identifiers", "EMBEDDED"],
    },
    {
      query: "C++ [x] a.b (go) $5 ^end? foo|bar path\\name {2} *",
      text: "C++ [x] a.b (go) $5 ^end? foo|bar path\\name {2} * axb foo bar",
      marks: ["C++", "[x]", "a.b", "(go)", "$5", "^end?", "foo|bar", "path\\name", "{2}", "*"],
    },
    { query: "ab abc", text: "abcABab", marks: ["ab", "AB", "ab"] },
    { query: "abc ab", text: "abcABab", marks: ["abc", "AB", "ab"] },
    { query: "café 東京 😀", text: "CAFÉ東京😀 café", marks: ["CAFÉ", "東京", "😀", "café"] },
    { query: "absent", text: "Original text stays intact.", marks: [] },
    { query: " \t ", text: "Original text stays intact.", marks: [] },
  ])("preserves literal highlight markup for $query", async ({ query, text, marks }) => {
    const { container } = renderSearch(
      {
        count: 1,
        total: 1,
        matches: [
          {
            type: "note",
            path: "docs/spec.md",
            title: text,
            snippetStatus: "available",
            snippet: text,
          },
        ],
      },
      query,
    );

    const title = await screen.findByRole("button", { name: text });
    const excerpt = container.querySelector(".search-workspace__result p");

    for (const element of [title, excerpt]) {
      expect(element?.textContent).toBe(text);
      expect(
        Array.from(element?.querySelectorAll("mark") ?? [], (mark) => mark.textContent),
      ).toEqual(marks);
    }
  });

  it("keeps missing excerpts honest and refines the current stable tab", async () => {
    const { onRefineSearch } = renderSearch({
      count: 1,
      total: 1,
      matches: [
        {
          type: "note",
          path: "docs/spec.md",
          title: "Embedded identifiers",
          snippetStatus: "unavailable",
        },
      ],
    });

    expect(await screen.findByText("Excerpt unavailable.")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Notes" }));
    expect(onRefineSearch).toHaveBeenCalledWith("search-tab:1", {
      scope: "notes",
      noteType: null,
      folder: null,
    });
  });

  it("appends continuation pages and navigates code to an available line", async () => {
    http.on("GET", "/api/v1/search", (request) => {
      const token = request.query.get("continuationToken");

      return jsonReply<WorkspaceSearchResponse>(
        token
          ? {
              count: 1,
              total: 2,
              matches: [
                {
                  type: "code",
                  path: "pkg/search/service.go",
                  title: "Service.Search",
                  startLine: 81,
                  snippetStatus: "available",
                  snippet: "func (s *Service) Search()",
                },
              ],
            }
          : {
              count: 1,
              total: 2,
              continuationToken: "page-two",
              matches: [
                {
                  type: "note",
                  path: "docs/spec.md",
                  title: "Search contract",
                  snippetStatus: "available",
                  snippet: "Search preserves ranked evidence.",
                },
              ],
            },
      );
    });
    renderSearch(null);

    await screen.findByRole("button", { name: "Search contract" });
    fireEvent.click(screen.getByRole("button", { name: "Load more results" }));
    const code = await screen.findByRole("button", { name: "Service.Search" });
    expect(screen.getByRole("button", { name: "Search contract" })).toBeVisible();
    expect(http.requests("GET", "/api/v1/search").at(-1)?.query.get("continuationToken")).toBe(
      "page-two",
    );

    fireEvent.click(code);
    await waitFor(() =>
      expect(window.location.href).toContain("/explorer?file=pkg%2Fsearch%2Fservice.go&line=81"),
    );
  });

  it("states ranked counts, target resolution, confidence, and coverage gaps", async () => {
    renderSearch({
      count: 1,
      total: 3,
      targetStatus: "inferred_symbol",
      confidence: { level: "medium", reason: "one strong source" },
      coverage: { missing: ["tests"] },
      matches: [
        {
          type: "note",
          path: "docs/spec.md",
          title: "Embedded identifiers",
          snippetStatus: "available",
          snippet: "Durable embedded identifiers keep links stable.",
        },
      ],
    });

    const status = await screen.findByLabelText("Search status");
    expect(status).toHaveTextContent("1 of 3 ranked");
    expect(status).toHaveTextContent("Exact symbol");
    expect(status).toHaveTextContent("Confidence medium");
    expect(status).toHaveTextContent("Missing: tests");
    expect(screen.getByText("Confidence medium")).toHaveAttribute("title", "one strong source");
  });

  it("shows only the ranked counts when retrieval reports no status signals", async () => {
    renderSearch();

    const status = await screen.findByLabelText("Search status");
    expect(status).toHaveTextContent("1 of 1 ranked");
    expect(status.textContent).toBe("1 of 1 ranked");
  });

  it.each([0, 1])(
    "does not report unavailable sources for interpretation notices with %s matches",
    async (count) => {
      renderSearch({
        count,
        total: count,
        matches: count ? [{ type: "note", path: "search.md", title: "Search" }] : [],
        warnings: [
          {
            code: "typo_fallback",
            kind: "query_interpretation",
            message: 'Included close title matches for "search".',
          },
        ],
      });
      expect(await screen.findByText('Included close title matches for "search".')).toBeVisible();
      expect(screen.queryByText(/Some search sources/)).toBeNull();
      expect(screen.queryByText("Search incomplete")).toBeNull();

      if (!count) expect(screen.getByText("No matches")).toBeVisible();
    },
  );

  it("reports availability warnings when no results are usable", async () => {
    renderSearch({
      count: 0,
      total: 0,
      matches: [],
      warnings: [
        {
          code: "indexed-context-missing",
          kind: "retrieval_config",
          message: "Indexed context is unavailable for this search.",
        },
      ],
    });

    expect(await screen.findByText("Search incomplete")).toBeVisible();
    expect(screen.getByText("Indexed context is unavailable for this search.")).toBeVisible();
    expect(screen.queryByText("No matches")).toBeNull();
  });

  it("explains lane-only degradation while retaining usable results", async () => {
    renderSearch({
      count: 1,
      total: 1,
      matches: [
        {
          type: "note",
          path: "docs/spec.md",
          title: "Search contract",
          snippetStatus: "available",
          snippet: "Usable lexical evidence remains available.",
        },
      ],
      lanes: [{ lane: "semantic", status: "timed_out" }],
      warnings: [],
    });

    expect(
      await screen.findByText("Some search sources are unavailable. Showing the usable results."),
    ).toBeVisible();
  });

  it("keeps cached rows visible and offers retry after a background refresh failure", async () => {
    let requests = 0;
    http.on("GET", "/api/v1/search", () => {
      requests += 1;

      if (requests > 1) throw new Error("refresh offline");

      return jsonReply<WorkspaceSearchResponse>({
        count: 1,
        total: 1,
        matches: [
          {
            type: "note",
            path: "docs/spec.md",
            title: "Cached contract",
            snippetStatus: "available",
            snippet: "Previously loaded evidence.",
          },
        ],
      });
    });
    const { queryClient } = renderSearch(null);
    expect(await screen.findByRole("button", { name: "Cached contract" })).toBeVisible();

    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.search.all() });
    });

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Search refresh failed. The previous results remain visible.",
    );
    expect(screen.getByRole("button", { name: "Retry refresh" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Cached contract" })).toBeVisible();
  });
});

describe("SearchWorkspace folder listing", () => {
  const http = withFakeFetch();
  const folderTab: SearchTab = { ...tab, query: "", filters: { ...tab.filters, folder: "Notes" } };

  function note(path: string, title: string, updatedAt: number, resolvedType?: string) {
    return { ref: { notePath: path }, path, title, updatedAt, resolvedType, hasIssues: false };
  }

  function renderFolder(
    notes = [
      note("Notes/old.md", "Old note", 1, "Spec"),
      note("Notes2/other.md", "Sibling folder note", 9),
      note("Notes/deep/new.md", "New note", 5),
      note("Archive/Notes/elsewhere.md", "Nested elsewhere", 7),
    ],
  ) {
    http.json("GET", "/api/v1/ontology/types/__all__", { count: notes.length, notes });
    const onRefineSearch = vi.fn();
    const onOpenNote = vi.fn();

    render(
      <SearchWorkspace
        tab={folderTab}
        active
        types={[]}
        onRefineSearch={onRefineSearch}
        onOpenNote={onOpenNote}
        onScrollPosition={vi.fn()}
      />,
    );

    return { onRefineSearch, onOpenNote };
  }

  it("lists the folder's notes newest first without running ranked search", async () => {
    const { onOpenNote } = renderFolder();

    expect(screen.getByRole("heading", { name: "Notes in Notes/" })).toBeVisible();
    expect(await screen.findByText("2 notes")).toBeVisible();
    const links = screen.getAllByRole("button", { name: /note$/ });
    expect(links.map((link) => link.textContent)).toEqual(["New note", "Old note"]);
    expect(screen.getByText("untyped")).toBeVisible();
    expect(screen.getByText("Notes/old.md")).toBeVisible();
    expect(screen.queryByText("Sibling folder note")).toBeNull();
    expect(http.count("GET", "/api/v1/search")).toBe(0);

    fireEvent.click(links[1]!);
    expect(onOpenNote).toHaveBeenCalledWith("Notes/old.md", "activate");
  });

  it("states an empty folder", async () => {
    renderFolder([note("Notes2/other.md", "Sibling folder note", 9)]);

    expect(await screen.findByText("No notes in Notes/")).toBeVisible();
  });

  it("offers retry when the note list fails", async () => {
    http.on("GET", "/api/v1/ontology/types/__all__", () => jsonReply({ error: "down" }, 500));
    render(
      <SearchWorkspace
        tab={folderTab}
        active
        types={[]}
        onRefineSearch={vi.fn()}
        onOpenNote={vi.fn()}
        onScrollPosition={vi.fn()}
      />,
    );

    expect(await screen.findByRole("alert")).toHaveTextContent("Notes unavailable");
  });

  it("turns into a ranked search when a query is entered and keeps the folder", async () => {
    const { onRefineSearch } = renderFolder();
    const input = screen.getByRole("textbox", { name: "Search this folder" });

    fireEvent.change(input, { target: { value: "plan" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onRefineSearch).toHaveBeenCalledWith("search-tab:1", folderTab.filters, "plan");

    const folder = screen.getByRole("textbox", { name: "Search folder" });
    fireEvent.change(folder, { target: { value: "" } });
    fireEvent.blur(folder);
    expect(folder).toHaveValue("Notes");
    expect(onRefineSearch).toHaveBeenCalledTimes(1);
  });
});
