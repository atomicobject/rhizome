// The probe must load before react-dom.
import { renderCount, resetRenderCounts } from "../test/renderProbe";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import ReactMarkdown from "react-markdown";
import { beforeEach, describe, expect, it } from "vitest";

import { isString } from "../api/parse";
import type { OntologyNoteListItem, OntologySummaryResponse } from "../api/types";
import { graphQLRequestBody, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { renderWithQueryClient as render } from "../test/renderWithQueryClient";
import { NotesShell } from "./NotesShell";
import { OntologyNotePane } from "./OntologyNotePane";

// Note-pane renders for one note and Markdown pipeline runs, so a shell state
// change that does not concern a retained tab shows up as wasted work.
const alphaRenders = () =>
  renderCount(OntologyNotePane, (props) => props.workspace?.node.notePath === "a.md");

const parses = () => renderCount(ReactMarkdown);

const summary: OntologySummaryResponse = {
  schemaPresent: true,
  totalNotes: 2,
  typedNotes: 2,
  untypedNotes: 0,
  ambiguousNotes: 0,
  issueNotes: 0,
  types: [{ name: "Plan", label: "Plan", count: 2, issueCount: 0, role: "note" }],
  interfaces: [],
};

function note(path: string, title: string): OntologyNoteListItem {
  return {
    ref: { notePath: path, kind: "NOTE" },
    path,
    title,
    resolvedType: "Plan",
    hasIssues: false,
    updatedAt: 1_713_000_000,
  };
}

const notes = [note("a.md", "Alpha"), note("b.md", "Beta")];

function nodeReply(ref: string) {
  return {
    data: {
      node: {
        ref: { ref, kind: "NOTE", notePath: ref, path: ref, fragment: null, nodeId: null },
        nodeId: `note:${ref}`,
        nodeKind: "NOTE",
        path: ref,
        title: ref,
        content: `# ${ref}\n\nSome **body** text with a [link](https://example.com).\n`,
        locator: { sourceLocator: ref, status: "linkable", exists: true, requiresFix: false },
      },
    },
  };
}

describe("NotesShell render boundaries", () => {
  const http = withFakeFetch();
  withFakeEventSource();

  beforeEach(() => {
    window.history.replaceState({}, "", "/notes");
    window.localStorage.clear();
    window.sessionStorage.clear();

    const all = {
      count: 2,
      issueCount: 0,
      type: { name: "All", label: "All", pluralLabel: "All", fields: [] },
      notes,
    };

    http
      .json("GET", "/api/v1/ontology/summary", summary)
      .json("GET", "/api/v1/ontology/types/__all__", all)
      .json("GET", "/api/v1/graphs/global", { nodes: [], edges: [], truncated: false })
      .json("GET", "/api/v1/views", { views: [] })
      .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
      .onGraphQL("PublicNodeDetail", (request) => {
        const ref = graphQLRequestBody(request).variables?.ref;

        return jsonReply(nodeReply(isString(ref) ? ref : ""));
      });
  });

  it("does not re-render or re-parse an inactive note tab when shell state changes", async () => {
    render(<NotesShell />);

    const open = (path: string) =>
      act(() => {
        window.history.pushState({}, "", `/notes?note=${path}`);
        window.dispatchEvent(new PopStateEvent("popstate"));
      });

    await screen.findByRole("button", { name: "All notes (2)" });
    open("a.md");
    await waitFor(() => expect(screen.getAllByText(/with a/)).toHaveLength(1));
    open("b.md");
    await waitFor(() => expect(screen.getAllByText(/with a/)).toHaveLength(2));
    // Let staged loads, titles, and context registration settle.
    await act(async () => new Promise((resolve) => setTimeout(resolve, 50)));

    // Opening the tabs rendered and parsed both notes, so the probe is live.
    expect(alphaRenders()).toBeGreaterThan(0);
    expect(parses()).toBeGreaterThan(0);
    resetRenderCounts();

    // Switching tabs changes nothing a retained, inactive tab reads.
    fireEvent.click(screen.getByRole("tab", { name: "Home" }));
    await waitFor(() => expect(window.location.search).toBe(""));
    fireEvent.click(screen.getByRole("tab", { name: /b\.md/i }));
    await waitFor(() => expect(window.location.search).toBe("?note=b.md"));
    await act(async () => new Promise((resolve) => setTimeout(resolve, 50)));

    const switchRenders = alphaRenders();
    const switchParses = parses();

    // The right pane's state lives in the shell, so hiding it re-renders no note pane.
    fireEvent.click(screen.getAllByRole("button", { name: "Hide right pane" })[0]!);
    await act(async () => new Promise((resolve) => setTimeout(resolve, 50)));

    const collapseRenders = alphaRenders() - switchRenders;
    const totalParses = parses();
    console.info(
      `[render-count] inactive tab: switch renders=${switchRenders} collapse renders=${collapseRenders}; markdown parses=${totalParses}`,
    );

    expect(switchRenders).toBe(0);
    expect(switchParses).toBe(0);
    expect(collapseRenders).toBe(0);
    expect(totalParses).toBe(0);
  });
});
