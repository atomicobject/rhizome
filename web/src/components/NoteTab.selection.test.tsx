import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";

import { isString } from "../api/parse";
import type { PublicNode } from "../api/publicGraphQLTypes";
import { deferredReply, graphQLRequestBody, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { withFakeEventSource } from "../test/fakeEventSource";
import { viewSegment } from "../test/workspaceView";
import { preferenceServer } from "../viewPreferences/testServer";
import type { ViewPreferenceResponse, ViewPreferenceScope } from "../viewPreferences/types";
import { NoteTab } from "./NoteTab";

// An untyped note resolves to the internal fallback type, which the UI hides.
function node(path: string): PublicNode {
  return {
    ref: { ref: path, notePath: path, path, kind: "NOTE", nodeId: `note:${path}` },
    nodeId: `note:${path}`,
    nodeKind: "NOTE",
    path,
    title: "Loose note",
    resolvedType: "_FallbackNote",
    content: "Loose narrative",
    locator: { sourceLocator: path, status: "linkable", exists: true, requiresFix: false },
  };
}

const noop = () => {};

function renderTab(path = "notes/loose.md") {
  return render(
    <NoteTab
      active
      anchor={null}
      editSession={null}
      editing={false}
      onOpen={noop}
      onTitle={noop}
      registerContext={noop}
      onStageOps={async () => {}}
      views={{ views: [], targets: [] }}
      vaultKey="vault-a"
      tab={{ id: `note:${path}`, kind: "note", path, dirty: false }}
    />,
  );
}

describe("note presentation selection persistence", () => {
  const http = withFakeFetch();
  withFakeEventSource();
  let server: ReturnType<typeof preferenceServer>;

  beforeEach(() => {
    server = preferenceServer(http);
    http.onGraphQL("PublicNodeDetail", (request) => {
      const value = graphQLRequestBody(request).variables?.ref;

      return jsonReply({ data: { node: node(isString(value) ? value : "notes/loose.md") } });
    });
  });

  it("remembers an untyped note's choice under its concrete fallback type", async () => {
    const first = renderTab();
    fireEvent.click(await screen.findByRole("button", { name: "Markdown" }));
    await waitFor(() => expect(server.writes).toHaveLength(1));
    expect(server.writes[0].scope.context).toMatchObject({
      kind: "node",
      type: "_FallbackNote",
      ref: { notePath: "notes/loose.md", kind: "NOTE" },
    });
    first.unmount();

    renderTab();
    expect(await screen.findByRole("textbox", { name: "Note source" })).toBeVisible();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("persists Source for untyped HTML with no resolved type or reference type", async () => {
    const path = "reports/untyped-report.htm";

    const html = {
      ...node(path),
      resolvedType: null,
      format: "html",
      content: '<html><body><script id="rhizome-metadata">Saved report</script></body></html>',
    };

    http.onGraphQL("PublicNodeDetail", () => jsonReply({ data: { node: html } }));
    const first = renderTab(path);
    fireEvent.click(await screen.findByRole("button", { name: "Source" }));
    await waitFor(() => expect(server.writes).toHaveLength(1));
    expect(server.writes[0].scope.context).toMatchObject({
      kind: "node",
      type: "_FallbackNote",
      ref: { notePath: path, kind: "NOTE" },
    });
    expect(await screen.findByLabelText("Note source")).toHaveTextContent("Saved report");
    first.unmount();
    renderTab(path);
    expect(await screen.findByLabelText("Note source")).toHaveTextContent("Saved report");
  });

  it("waits for the remembered choice before showing the default presentation", async () => {
    const remembered = deferredReply<ViewPreferenceResponse>();
    let scope: ViewPreferenceScope | null = null;

    http.on("GET", "/api/v1/view-preferences", (request) => {
      scope = JSON.parse(request.query.get("scope") ?? "null");

      return remembered.promise;
    });
    renderTab();
    expect(await screen.findByText("Loading view…")).toBeVisible();
    expect(screen.queryByText("Loose narrative")).toBeNull();

    remembered.resolve({
      ...server.read(scope!),
      revision: 1,
      values: { choice: "builtin:source" },
    });
    expect(await screen.findByRole("textbox", { name: "Note source" })).toBeVisible();
    expect(screen.queryByText("Loading view…")).toBeNull();
  });

  it("shows a failed save and retries it", async () => {
    renderTab();
    await screen.findByRole("button", { name: "Markdown" });
    server.failWrites("disk full");
    fireEvent.click(viewSegment("Markdown"));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("View choice not saved.");
    expect(viewSegment("Markdown")).toHaveAttribute("aria-pressed", "true");

    server.failWrites(null);
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    expect(server.writes.map((write) => write.set)).toEqual([{ choice: "builtin:source" }]);
  });
});
