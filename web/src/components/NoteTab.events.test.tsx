import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";

import { isString } from "../api/parse";
import type {
  PublicGraphQLResult,
  PublicNode,
  PublicNodeDetailData,
} from "../api/publicGraphQLTypes";
import type { NodeEvent } from "../api/types";
import { deferredReply, graphQLRequestBody, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { NoteTab } from "./NoteTab";
import { splitNoteTarget } from "./notesRoute";
import { viewSegment } from "../test/workspaceView";

type Reply = PublicGraphQLResult<PublicNodeDetailData>;

function nodeReply(path: string, section = false, updated = false): Reply {
  const ref = {
    ref: section ? `${path}#details` : path,
    kind: section ? "SECTION" : "NOTE",
    notePath: path,
    path,
    fragment: section ? "details" : null,
  };

  const title = section ? "Details" : path === "notes/a.md" ? "Alpha" : "Beta";

  const content = section
    ? updated
      ? "Updated section body"
      : "Original section body"
    : path === "notes/a.md"
      ? "[Drill into details](rhizome://note/notes%2Fa.md%23details)"
      : "Beta stays unchanged";

  const node: PublicNode = {
    ref,
    nodeId: section ? `${path}#details` : `note:${path}`,
    nodeKind: ref.kind,
    path,
    title,
    resolvedType: section ? "_FallbackSection" : "_FallbackNote",
    content,
    locator: { sourceLocator: ref.ref, status: "linkable", exists: true, requiresFix: false },
    workspace: {
      fields: [],
      collections: [],
      structure: [],
      relationGroups: [],
      capabilities: {
        canEdit: false,
        canEditFields: false,
        canEditCollections: false,
        canNavigateChildren: true,
        canSubscribe: true,
      },
      status: {
        dirty: false,
        validation: { issueCount: 0 },
        freshness: {},
        session: {},
        hasWarnings: false,
      },
      version: updated ? "v2" : "v1",
      loaded: { rendered: true, assessment: false, structure: true, relations: false },
      bodies: [
        {
          ref,
          title,
          locator: section ? "SECTION" : "FILE",
          markdown: content,
          fields: [],
          collections: [],
          blocks: [
            {
              kind: "narrative",
              markdown: content,
              range: { start: 0, end: content.length },
              childRefs: [],
            },
          ],
        },
      ],
    },
  };

  return { data: { node } };
}

const noop = () => {};

const stage = async () => {};

function ControlledTab({ path }: { path: string }) {
  const [anchor, setAnchor] = useState<string | null>(null);

  return (
    <NoteTab
      tab={{ id: `note:${path}`, kind: "note", path, dirty: false }}
      active={true}
      anchor={anchor}
      editSession={null}
      editing={false}
      onOpen={(target) => setAnchor(splitNoteTarget(target).fragment)}
      onTitle={noop}
      registerContext={noop}
      contextCollapsed={false}
      onToggleContext={noop}
      onStageOps={stage}
    />
  );
}

describe("note tab event isolation", () => {
  const http = withFakeFetch();
  const sources = withFakeEventSource();

  it("refreshes only the stale node while retaining its tab mode, drill and scroll", async () => {
    const refresh = deferredReply<Reply>();
    let sectionReads = 0;
    const reads: string[] = [];
    http.onGraphQL("PublicNodeDetail", (request) => {
      const value = graphQLRequestBody(request).variables?.ref;
      const ref = isString(value) ? value : "";
      reads.push(ref);

      if (ref === "notes/a.md#details") {
        sectionReads += 1;

        return sectionReads === 1 ? jsonReply(nodeReply("notes/a.md", true)) : refresh.promise;
      }

      return jsonReply(nodeReply(ref));
    });
    render(
      <>
        <section aria-label="Alpha tab">
          <ControlledTab path="notes/a.md" />
        </section>
        <section aria-label="Beta tab">
          <ControlledTab path="notes/b.md" />
        </section>
      </>,
    );
    const alpha = within(screen.getByRole("region", { name: "Alpha tab" }));
    const beta = within(screen.getByRole("region", { name: "Beta tab" }));
    fireEvent.click(await alpha.findByRole("link", { name: "Drill into details" }));
    await alpha.findByText("Original section body");
    fireEvent.click(viewSegment("Markdown", alpha));
    await waitFor(() =>
      expect(viewSegment("Markdown", alpha)).toHaveAttribute("aria-pressed", "true"),
    );

    const body = screen
      .getByRole("region", { name: "Alpha tab" })
      .querySelector(".ontology-pane__main");

    if (!(body instanceof HTMLElement)) throw new Error("Expected the note reading surface");
    body.scrollTop = 135;

    const event: NodeEvent = {
      id: "stale-alpha-details",
      kind: "node.stale",
      ref: { kind: "SECTION", notePath: "notes/a.md", fragment: "details" },
    };

    act(() => sources.latest().emitJSON(event));
    await waitFor(() => expect(sectionReads).toBe(2));
    expect(viewSegment("Markdown", alpha)).toHaveAttribute("aria-pressed", "true");
    expect(alpha.getByRole("navigation", { name: "Node path" })).toHaveTextContent("Alpha›Details");
    expect(beta.getByText("Beta stays unchanged")).toBeInTheDocument();
    await act(async () => refresh.resolve(nodeReply("notes/a.md", true, true)));
    await alpha.findByText("Updated section body");
    expect(viewSegment("Markdown", alpha)).toHaveAttribute("aria-pressed", "true");
    expect(alpha.getByRole("navigation", { name: "Node path" })).toHaveTextContent("Alpha›Details");
    expect(body.isConnected).toBe(true);
    expect(body.scrollTop).toBe(135);
    expect(reads).toEqual(["notes/a.md", "notes/b.md", "notes/a.md#details", "notes/a.md#details"]);
    expect(sources.instances.filter((source) => !source.closed)).toHaveLength(1);
  });
});
