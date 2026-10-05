import { screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { isJsonObject } from "../api/parse";
import { queryKeys } from "../api/queryKeys";
import type { OntologyEditSessionResponse, ViewExecuteRequest } from "../api/types";
import { baseView, execution } from "../components/ConfiguredView.testFixtures";
import { ViewTab } from "../components/ViewTab";
import {
  decodeJSONBody,
  emptyValidationSummariesReply,
  jsonReply,
  withFakeFetch,
} from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { nativePreferenceScope } from "../viewPreferences/native";
import { getPreferenceStore } from "../viewPreferences/store";
import { preferenceServer } from "../viewPreferences/testServer";

const EXECUTE = `/api/v1/views/${baseView.id}/execute`;

const SCOPE = nativePreferenceScope(baseView, { kind: "standalone" });

const kept = { field: "title", op: "contains", value: "alpha" };

const gone = { field: "retired", op: "eq", value: "x" };

/** The default execution of a type view, whose schema fields prove which bare names exist. */
function defaults(schemaFields: string[]) {
  const base = execution();

  return {
    ...base,
    capabilities: [
      ...(base.capabilities ?? []),
      ...schemaFields.map((key) => ({ key, source: "schema", sortable: true, groupable: true })),
    ],
  };
}

const staged: OntologyEditSessionResponse = {
  sessionId: "s1",
  status: "dirty",
  revision: 1,
  hasUncommittedChanges: true,
  createdAt: "2026-10-04T00:00:00Z",
  updatedAt: "2026-10-04T00:00:01Z",
};

function isRequest(value: unknown): value is ViewExecuteRequest {
  return isJsonObject(value);
}

describe("NativeCollectionView remembered fields", () => {
  const http = withFakeFetch();

  function open(probe: () => Response) {
    http
      .json("GET", "/api/v1/views", { views: [baseView] })
      .json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 })
      .on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply)
      .on("POST", EXECUTE, (request) =>
        decodeJSONBody(request, isRequest).filters ? jsonReply(execution()) : probe(),
      );

    const tab = (mount: string, session: OntologyEditSessionResponse | null) => (
      <ViewTab
        key={mount}
        tab={{ id: `view:${baseView.id}`, kind: "view", viewId: baseView.id, title: "Specs" }}
        active
        editSession={{ session, vaultKey: "vault" }}
        onOpenNote={vi.fn()}
        onStageOps={async () => undefined}
        onOpenIssues={vi.fn()}
        onTitle={vi.fn()}
      />
    );

    const rendered = renderWithQueryClient(tab("first", null));

    return {
      queryClient: rendered.queryClient,
      remount: () => rendered.rerender(tab("second", null)),
      stage: () => rendered.rerender(tab("first", staged)),
    };
  }

  const executed = () =>
    http.requests("POST", EXECUTE).map((request) => decodeJSONBody(request, isRequest));

  const probes = () => executed().filter((request) => !request.filters).length;

  it("drops a filter on a field the default execution proves gone, before running the query", async () => {
    const server = preferenceServer(http);
    server.change(SCOPE, { "native.filters": [kept, gone] });
    open(() => jsonReply(defaults(["specStatus"])));

    await waitFor(() => expect(executed().some((request) => request.filters)).toBe(true));
    expect(executed().find((request) => request.filters)?.filters).toEqual([kept]);
    await waitFor(() => expect(server.read(SCOPE).values["native.filters"]).toEqual([kept]));
  });

  it("runs the saved request unchanged when the default execution fails", async () => {
    const server = preferenceServer(http);
    server.change(SCOPE, { "native.filters": [kept, gone] });
    open(() => jsonReply({ error: "board axis is not a field" }, 400));

    await waitFor(() => expect(executed().some((request) => request.filters)).toBe(true));
    expect(executed().find((request) => request.filters)?.filters).toEqual([kept, gone]);
    expect(server.read(SCOPE).values["native.filters"]).toEqual([kept, gone]);
    expect(await screen.findByRole("heading", { name: "Spec Table" })).toBeVisible();
  });

  it("judges restored fields only by a settled execution, never a cached one", async () => {
    const server = preferenceServer(http);
    let schema = ["specStatus"];
    const view = open(() => jsonReply(defaults(schema)));
    await waitFor(() => expect(probes()).toBe(1));

    // The schema gains a field and the reader filters on it; the cached defaults predate it.
    schema = ["specStatus", "added"];
    const added = { field: "added", op: "eq", value: "x" };
    await getPreferenceStore(SCOPE, "vault").patch({ set: { "native.filters": [added] } });
    view.remount();

    await waitFor(() => expect(probes()).toBe(2));
    await waitFor(() => expect(executed().some((request) => request.filters)).toBe(true));
    expect(executed().find((request) => request.filters)?.filters).toEqual([added]);
    expect(server.read(SCOPE).values["native.filters"]).toEqual([added]);
  });

  it("checks restored fields once, not on every index event or staged session", async () => {
    const server = preferenceServer(http);
    server.change(SCOPE, { "native.filters": [kept, gone] });
    const view = open(() => jsonReply(defaults(["specStatus"])));
    const main = () => executed().filter((request) => request.filters).length;
    await waitFor(() => expect(main()).toBe(1));

    await view.queryClient.invalidateQueries({ queryKey: queryKeys.views.all() });
    await waitFor(() => expect(main()).toBe(2));
    view.stage();
    await waitFor(() => expect(main()).toBe(3));

    expect(probes()).toBe(1);
    expect(executed().at(-1)?.filters).toEqual([kept]);
  });
});
