import { QueryClient, QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, expect, it, vi } from "vitest";

import type { OntologyEditSessionResponse } from "../src/api/types";
import { INITIAL_EDIT_READ_LIFECYCLE } from "../src/staging/stagedQuery";
import { deferredReply, graphQLRequestBody, jsonReply, withFakeFetch } from "../src/test/fakeFetch";
import { withFakeEventSource } from "../src/test/fakeEventSource";
import { mountView } from "./index";
import {
  EDIT_SESSION_MESSAGE,
  OPEN_COLLECTION_MESSAGE,
  OPEN_ISSUES_MESSAGE,
  VAULT_EVENT_MESSAGE,
  type HostMessage,
} from "../src/lib/customViewMessages";

const http = withFakeFetch();

const events = withFakeEventSource();

const parentDescriptor = Object.getOwnPropertyDescriptor(window, "parent")!;

const saved: OntologyEditSessionResponse = {
  sessionId: "kit-save",
  revision: 1,
  status: "dirty",
  hasUncommittedChanges: true,
  createdAt: "2026-10-02T00:00:00Z",
  updatedAt: "2026-10-02T00:00:00Z",
  ops: [{ kind: "setField", path: "a.md", field: "rank", value: "4" }],
};

afterEach(() => {
  Object.defineProperty(window, "parent", parentDescriptor);
  vi.restoreAllMocks();
});

it("keeps optimistic data after Save, passes cancellation, and adopts canonical data once", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );

  const frame = document.createElement("iframe");
  document.body.append(frame);
  const parent = frame.contentWindow!;
  Object.defineProperty(window, "parent", { configurable: true, value: parent });
  vi.resetModules();
  const { useGraphQL } = await import("./index");

  const answer = (message: HostMessage) =>
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          source: parent,
          origin: window.location.origin,
          data: message,
        }),
      );
    });

  const route = {
    when: (request: import("../src/test/fakeFetch").FakeFetchRequest) =>
      graphQLRequestBody(request).query === "query Rank { rank }",
  };

  const staged = deferredReply<{ data: { rank: number } }>();
  const committed = deferredReply<{ data: { rank: number } }>();
  http.on("POST", "/api/v1/graphql", () => jsonReply({ data: { rank: 1 } }), route);
  http.onGraphQL("Other", () => jsonReply({ data: { rank: 99 } }));

  const optimistic = vi.fn((data: { rank: number }, session: typeof saved | null) => ({
    ...data,
    rank: Number(session?.ops?.at(-1)?.value ?? data.rank),
  }));

  const { result } = renderHook(
    () => useGraphQL<{ rank: number }>("query Rank { rank }", undefined, { optimistic }),
    { wrapper },
  );

  await waitFor(() => expect(result.current.data).toEqual({ rank: 1 }));
  http.on("POST", "/api/v1/graphql", () => staged.promise, route);
  answer({
    type: EDIT_SESSION_MESSAGE,
    session: saved,
    readLifecycle: INITIAL_EDIT_READ_LIFECYCLE,
  });
  await waitFor(() => expect(http.requests("POST", "/api/v1/graphql")).toHaveLength(2));
  expect(result.current.data).toEqual({ rank: 4 });
  http.on("POST", "/api/v1/graphql", () => committed.promise, route);
  answer({
    type: EDIT_SESSION_MESSAGE,
    session: null,
    readLifecycle: { revision: 1, outcome: "saved", savedSession: saved },
  });
  await waitFor(() => expect(http.requests("POST", "/api/v1/graphql")).toHaveLength(3));
  expect(http.requests("POST", "/api/v1/graphql")[1].signal?.aborted).toBe(true);
  expect(result.current.data).toEqual({ rank: 4 });
  expect(result.current.editOutcome).toBe("saved");
  expect(result.current.savedEditsPending).toBe(true);
  staged.resolve({ data: { rank: 2 } });
  committed.resolve({ data: { rank: 5 } });
  await waitFor(() => expect(result.current.data).toEqual({ rank: 5 }));
  expect(result.current.savedEditsPending).toBe(false);
  // The updater always receives server data, never its previous patched result.
  expect(optimistic.mock.calls.every(([data]) => data.rank === 1)).toBe(true);

  const newer = {
    ...saved,
    revision: 2,
    ops: [{ kind: "setField", path: "a.md", field: "rank", value: "6" }],
  };

  answer({
    type: EDIT_SESSION_MESSAGE,
    session: newer,
    readLifecycle: { revision: 1, outcome: "failed", savedSession: saved },
  });
  await waitFor(() => expect(result.current.data).toEqual({ rank: 6 }));
  expect(result.current.editOutcome).toBe("failed");
  expect(result.current.savedEditsPending).toBe(false);

  answer({
    type: EDIT_SESSION_MESSAGE,
    session: null,
    readLifecycle: { revision: 2, outcome: "discarded", savedSession: null },
  });
  await waitFor(() => expect(result.current.data).toEqual({ rank: 5 }));
  expect(result.current.editOutcome).toBe("discarded");
});

it("keeps data that arrives with field errors only when a query asks for partial results", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );

  vi.resetModules();
  const { useGraphQL } = await import("./index");

  const reply = {
    data: { notes: [{ path: "a.md" }] },
    errors: [{ message: "required field summary is missing", path: ["notes", "summary"] }],
  };

  http.on("POST", "/api/v1/graphql", () => jsonReply(reply));

  const partial = renderHook(
    () => useGraphQL("query Notes { notes { path summary } }", undefined, { partial: true }),
    { wrapper },
  );

  const strict = renderHook(() => useGraphQL("query Notes { notes { path summary } }"), {
    wrapper,
  });

  await waitFor(() => expect(partial.result.current.data).toEqual(reply.data));
  await waitFor(() =>
    expect(strict.result.current.error?.message).toBe("required field summary is missing"),
  );

  http.on("POST", "/api/v1/graphql", () =>
    jsonReply({ data: null, errors: [{ message: "index offline" }] }),
  );

  const missing = renderHook(
    () => useGraphQL("query Other { notes { path } }", undefined, { partial: true }),
    { wrapper },
  );

  await waitFor(() => expect(missing.result.current.error?.message).toBe("index offline"));

  // A root that failed outright, or an error without a path, is not partial data.
  http.on("POST", "/api/v1/graphql", () =>
    jsonReply({ data: { notes: null }, errors: [{ message: "notes failed", path: ["notes"] }] }),
  );

  const failedRoot = renderHook(
    () => useGraphQL("query Root { notes { path } }", undefined, { partial: true }),
    { wrapper },
  );

  await waitFor(() => expect(failedRoot.result.current.error?.message).toBe("notes failed"));
  http.on("POST", "/api/v1/graphql", () =>
    jsonReply({ data: { notes: [] }, errors: [{ message: "timeout" }] }),
  );

  const pathless = renderHook(
    () => useGraphQL("query Pathless { notes { path } }", undefined, { partial: true }),
    { wrapper },
  );

  await waitFor(() => expect(pathless.result.current.error?.message).toBe("timeout"));
});

it("refreshes kit queries after index invalidation and reconnect", async () => {
  document.body.innerHTML = '<div id="root"></div>';
  let client: QueryClient | undefined;

  const View = () => {
    client = useQueryClient();

    return null;
  };

  await act(async () => {
    await mountView(async () => ({ default: View }), { id: "freshness", name: "Freshness" });
  });
  expect(client).toBeDefined();
  const invalidate = vi.spyOn(client!, "invalidateQueries");
  const stream = events.latest();
  stream.open();
  expect(invalidate).not.toHaveBeenCalled();
  stream.emit("", "index.invalidated");
  await waitFor(() => expect(invalidate).toHaveBeenCalledTimes(1));
  stream.open();
  await waitFor(() => expect(invalidate).toHaveBeenLastCalledWith());
});

it("takes a hosted view's freshness from the workspace without polling its folder", async () => {
  document.body.innerHTML = '<div id="root"></div>';
  const frame = document.createElement("iframe");
  document.body.append(frame);
  const parent = frame.contentWindow!;
  Object.defineProperty(window, "parent", { configurable: true, value: parent });
  vi.resetModules();
  const kit = await import("./index");
  const poll = vi.spyOn(window, "setInterval");
  http.on("GET", "/views/_stamp/board", () => new Response("1"));
  let client: QueryClient | undefined;

  const View = () => {
    client = useQueryClient();

    return null;
  };

  await act(async () => {
    await kit.mountView(async () => ({ default: View }), {
      id: "board",
      name: "Board",
      stamp: "/views/_stamp/board",
      stampValue: "1",
    });
  });
  expect(events.instances).toHaveLength(0);
  expect(poll).not.toHaveBeenCalled();
  // One check against the served baseline replaces the poll.
  await waitFor(() => expect(http.requests("GET", "/views/_stamp/board")).toHaveLength(1));

  const invalidate = vi.spyOn(client!, "invalidateQueries");
  window.dispatchEvent(
    new MessageEvent("message", {
      source: parent,
      origin: window.location.origin,
      data: { type: VAULT_EVENT_MESSAGE, event: "node.changed" },
    }),
  );
  await waitFor(() => expect(invalidate).toHaveBeenCalledTimes(1));
});

it("asks the hosting workspace to open issues and collections", async () => {
  const frame = document.createElement("iframe");
  document.body.append(frame);
  const parent = frame.contentWindow!;
  Object.defineProperty(window, "parent", { configurable: true, value: parent });
  vi.resetModules();
  const kit = await import("./index");
  const sent = vi.spyOn(parent, "postMessage");

  kit.openIssues({ kind: "interface", key: "Work" });
  kit.openIssues();
  kit.openCollection("Spec");

  expect(sent.mock.calls).toEqual([
    [
      { type: OPEN_ISSUES_MESSAGE, scope: { kind: "interface", key: "Work" } },
      window.location.origin,
    ],
    [{ type: OPEN_ISSUES_MESSAGE }, window.location.origin],
    [{ type: OPEN_COLLECTION_MESSAGE, name: "Spec" }, window.location.origin],
  ]);
});

it("keeps data returned beside GraphQL errors only when asked to", async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );

  vi.resetModules();
  const { useGraphQL } = await import("./index");
  // The server names the root and field of a missing required field.
  const missing = [{ message: "required field summary is missing", path: ["specs", "summary"] }];

  const asking = (query: string) => ({
    when: (request: import("../src/test/fakeFetch").FakeFetchRequest) =>
      graphQLRequestBody(request).query === query,
  });

  http.on(
    "POST",
    "/api/v1/graphql",
    () => jsonReply({ data: { specs: [{ title: "A" }] }, errors: missing }),
    asking("query Specs { specs { title summary } }"),
  );
  http.on(
    "POST",
    "/api/v1/graphql",
    () => jsonReply({ data: null, errors: missing }),
    asking("query Broken { specs { summary } }"),
  );

  const { result } = renderHook(
    () => ({
      strict: useGraphQL("query Specs { specs { title summary } }"),
      partial: useGraphQL("query Specs { specs { title summary } }", undefined, { partial: true }),
      empty: useGraphQL("query Broken { specs { summary } }", undefined, { partial: true }),
    }),
    { wrapper },
  );

  await waitFor(() => expect(result.current.partial.data).toEqual({ specs: [{ title: "A" }] }));
  // The tolerated errors stay readable, so a view can tell which records lost a field.
  expect(result.current.partial.partialErrors).toEqual(missing);
  await waitFor(() => expect(result.current.strict.error?.message).toBe(missing[0].message));
  expect(result.current.strict.partialErrors).toEqual([]);

  // An equal response keeps the data object on screen but not the old errors.
  const shown = result.current.partial.data;
  http.on(
    "POST",
    "/api/v1/graphql",
    () => jsonReply({ data: { specs: [{ title: "A" }] } }),
    asking("query Specs { specs { title summary } }"),
  );
  await act(() => client.invalidateQueries());
  await waitFor(() => expect(result.current.partial.partialErrors).toEqual([]));
  expect(result.current.partial.data).toBe(shown);
  await waitFor(() => expect(result.current.empty.error?.message).toBe(missing[0].message));
});

it("counts a frame as hosted only when its parent is the same origin", async () => {
  const frame = document.createElement("iframe");
  document.body.append(frame);
  Object.defineProperty(window, "parent", { configurable: true, value: frame.contentWindow });
  vi.resetModules();
  expect((await import("./index")).embedded).toBe(true);

  // An IDE preview or other cross-origin page framing /views/<id> refuses
  // access to its location, so the view runs as its own page.
  const foreign = {
    get location(): Location {
      throw new DOMException("Blocked a frame with origin", "SecurityError");
    },
  };

  Object.defineProperty(window, "parent", { configurable: true, value: foreign });
  vi.resetModules();
  expect((await import("./index")).embedded).toBe(false);

  Object.defineProperty(window, "parent", parentDescriptor);
  vi.resetModules();
  expect((await import("./index")).embedded).toBe(false);
});
