import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";

import { deferredReply, jsonReply, withFakeFetch } from "../src/test/fakeFetch";
import { INITIAL_EDIT_READ_LIFECYCLE } from "../src/staging/stagedQuery";
import type { OntologyEditSessionResponse } from "../src/api/types";

import { EDIT_SESSION_MESSAGE } from "../src/lib/customViewMessages";

const originalParent = window.parent;

const http = withFakeFetch();

afterEach(() => {
  Object.defineProperty(window, "parent", { configurable: true, value: originalParent });
  document.body.replaceChildren();
  vi.unstubAllGlobals();
});

it("reuses the native executor and reads through the host edit session without exposing it in the launch URL", async () => {
  const frame = document.createElement("iframe");
  document.body.append(frame);
  const parent = frame.contentWindow!;

  Object.defineProperty(window, "parent", { configurable: true, value: parent });
  const requests: Array<{ url: string; body: string }> = [];

  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      requests.push({ url, body: String(init.body) });

      return new Response(
        JSON.stringify({
          rows: [{ nodeId: "story", title: requests.length === 1 ? "Committed" : "Staged" }],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }),
  );
  const { useViewRows } = await import("./index");
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );

  const { result, unmount } = renderHook(
    () => useViewRows("native.stories", { variant: "table" }),
    { wrapper },
  );

  await waitFor(() => expect(result.current.data?.rows[0]?.title).toBe("Committed"));
  window.dispatchEvent(
    new MessageEvent("message", {
      source: parent,
      origin: window.location.origin,
      data: {
        type: EDIT_SESSION_MESSAGE,
        session: {
          sessionId: "workspace-session",
          revision: 2,
          hasUncommittedChanges: true,
          ops: [
            {
              kind: "setField",
              path: "stories.md#^story",
              field: "title",
              fieldValue: { kind: "scalar", scalar: "Staged" },
            },
          ],
          baseFingerprints: { "stories.md": "base" },
          baseDocuments: [],
        },
      },
    }),
  );
  await waitFor(() => expect(result.current.data?.rows[0]?.title).toBe("Staged"));
  expect(requests).toHaveLength(2);
  expect(requests[0]).toEqual({
    url: "/api/v1/views/native.stories/execute",
    body: '{"variant":"table"}',
  });
  expect(JSON.parse(requests[1].body).editSession.snapshot).toMatchObject({
    sessionId: "workspace-session",
    revision: 2,
    ops: [{ path: "stories.md#^story" }],
  });
  expect(window.location.search).not.toContain("workspace-session");
  unmount();
  client.clear();
});

it("holds native rows through save and cancels obsolete reads before adopting canonical rows", async () => {
  const frame = document.createElement("iframe");
  document.body.append(frame);
  const parent = frame.contentWindow!;
  Object.defineProperty(window, "parent", { configurable: true, value: parent });
  vi.resetModules();
  const { useViewRows } = await import("./index");
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );

  const staged = deferredReply<{ rows: { title: string }[] }>();
  const canonical = deferredReply<{ rows: { title: string }[] }>();
  const path = "/api/v1/views/native.lifecycle/execute";
  http.on("POST", path, () => jsonReply({ rows: [{ title: "Before" }] }));

  const saved: OntologyEditSessionResponse = {
    sessionId: "native-save",
    revision: 1,
    status: "dirty",
    hasUncommittedChanges: true,
    createdAt: "2026-10-02T00:00:00Z",
    updatedAt: "2026-10-02T00:00:00Z",
    ops: [{ kind: "setField", path: "a.md", field: "title", value: "Saved" }],
  };

  const { result, unmount } = renderHook(
    () =>
      useViewRows(
        "native.lifecycle",
        {},
        {
          optimistic: (data, session) => ({
            ...data,
            rows: data.rows.map((row) => ({
              ...row,
              title: session?.ops?.at(-1)?.value ?? row.title,
            })),
          }),
        },
      ),
    { wrapper },
  );

  const answer = (session: OntologyEditSessionResponse | null, savedRevision = false) =>
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          source: parent,
          origin: window.location.origin,
          data: {
            type: EDIT_SESSION_MESSAGE,
            session,
            readLifecycle: savedRevision
              ? { revision: 1, outcome: "saved", savedSession: saved }
              : INITIAL_EDIT_READ_LIFECYCLE,
          },
        }),
      );
    });

  await waitFor(() => expect(result.current.data?.rows[0].title).toBe("Before"));
  http.on("POST", path, () => staged.promise);
  answer(saved);
  await waitFor(() => expect(http.requests("POST", path)).toHaveLength(2));
  expect(result.current.data?.rows[0].title).toBe("Saved");
  http.on("POST", path, () => canonical.promise);
  answer(null, true);
  await waitFor(() => expect(http.requests("POST", path)).toHaveLength(3));
  expect(http.requests("POST", path)[1].signal?.aborted).toBe(true);
  expect(result.current.data?.rows[0].title).toBe("Saved");
  expect(result.current.displaySession?.sessionId).toBe(saved.sessionId);
  expect(result.current.savedEditsPending).toBe(true);
  expect(result.current.editOutcome).toBe("saved");
  staged.resolve({ rows: [{ title: "Obsolete" }] });
  canonical.resolve({ rows: [{ title: "Canonical" }] });
  await waitFor(() => expect(result.current.data?.rows[0].title).toBe("Canonical"));
  expect(result.current.savedEditsPending).toBe(false);
  expect(result.current.displaySession).toBeNull();
  unmount();
  client.clear();
});
