import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { OntologyEditOp, OntologyEditSessionResponse } from "../api/types";
import { deferredReply, jsonReply, withFakeFetch } from "../test/fakeFetch";
import { OntologyEditSessionProvider, useOntologyEditSession } from "./useOntologyEditSession";
import { remapCommittedEditOps } from "./editing/editSessionState";

const http = withFakeFetch();

function setField(value: string): OntologyEditOp {
  return { kind: "setField", path: "notes/a.md", field: "x", value };
}

function sessionResponse(): OntologyEditSessionResponse {
  return {
    sessionId: "edit-1",
    status: "dirty",
    ops: [],
    baseFingerprints: {},
    hasUncommittedChanges: true,
    createdAt: "2026-09-11T00:00:00Z",
    updatedAt: "2026-09-11T00:00:00Z",
  };
}

function wrapper({ children }: PropsWithChildren) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return (
    <QueryClientProvider client={client}>
      <OntologyEditSessionProvider vaultKey="/test-vault">{children}</OntologyEditSessionProvider>
    </QueryClientProvider>
  );
}

describe("useOntologyEditSession", () => {
  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
  });

  it("reports busy while a stored session is being restored", async () => {
    sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "restore-test");
    localStorage.setItem(
      "rhizome:ontology-edit-session:%2Ftest-vault:restore-test",
      JSON.stringify({
        version: 3,
        sessionId: "edit-1",
        ops: [setField("draft")],
        baseFingerprints: {},
        baseDocuments: [],
      }),
    );
    const restore = deferredReply<ReturnType<typeof sessionResponse>>();
    http.on("POST", "/api/v1/edit-sessions/edit-1/preview", () => restore.promise);

    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });
    expect(result.current.busy).toBe(true);

    await act(async () => {
      restore.resolve(sessionResponse());
    });
    await waitFor(() => expect(result.current.busy).toBe(false));
  });

  it("stays busy until every queued edit operation settles", async () => {
    const create = deferredReply<ReturnType<typeof sessionResponse>>();
    const stage = deferredReply<ReturnType<typeof sessionResponse>>();
    http.on("POST", "/api/v1/edit-sessions", () => create.promise);
    http.on("POST", "/api/v1/edit-sessions/edit-1/stage", () => stage.promise);
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });

    let first!: Promise<OntologyEditSessionResponse | null>;
    let second!: Promise<OntologyEditSessionResponse | null>;
    act(() => {
      first = result.current.stageOps([setField("1")]);
      second = result.current.stageOps([setField("2")]);
    });
    expect(result.current.busy).toBe(true);

    await act(async () => {
      create.resolve(sessionResponse());
      await first;
    });
    expect(result.current.busy).toBe(true);

    await act(async () => {
      stage.resolve(sessionResponse());
      await second;
    });
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(http.count("POST", "/api/v1/edit-sessions")).toBe(1);
    expect(http.count("POST", "/api/v1/edit-sessions/edit-1/stage")).toBe(1);
  });

  it("keeps successful server edits when browser storage throws", async () => {
    http.json("POST", "/api/v1/edit-sessions", sessionResponse());

    const storageSpy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });

    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });

    await act(async () => {
      await result.current.stageOps([setField("1")]);
    });
    expect(result.current.session?.sessionId).toBe("edit-1");
    expect(result.current.warnings).toContain(
      "Browser storage unavailable; edits remain active for this tab.",
    );
    storageSpy.mockRestore();
  });

  it("persists the first compact operation before the server acknowledges it", async () => {
    sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "draft-test");
    const create = deferredReply<ReturnType<typeof sessionResponse>>();
    http.on("POST", "/api/v1/edit-sessions", () => create.promise);
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });

    let pending!: Promise<OntologyEditSessionResponse | null>;
    act(() => {
      pending = result.current.stageOps([
        {
          ...setField("draft"),
          id: "field:notes/a.md:x",
          expected: {
            sourceHash: "base-hash",
            sourceContent: "---\nx: old\n---\n",
          },
        },
      ]);
    });

    const stored = JSON.parse(
      localStorage.getItem("rhizome:ontology-edit-session:%2Ftest-vault:draft-test") || "null",
    );

    expect(stored).toMatchObject({
      version: 3,
      ops: [{ id: "field:notes/a.md:x", value: "draft" }],
      baseDocuments: [
        {
          notePath: "notes/a.md",
          fingerprint: "base-hash",
          content: "---\nx: old\n---\n",
        },
      ],
    });

    await act(async () => {
      create.resolve(sessionResponse());
      await pending;
    });
  });

  it("adopts canonical server refs without restaging acknowledged edits on Save", async () => {
    const local = {
      ...setField("blocked"),
      id: "field-status",
      nodeId: "notes/a.md#item-1",
      structuralFingerprint: "preview-fingerprint",
    };

    const canonical = { ...local, structuralFingerprint: "base-fingerprint" };
    http.json("POST", "/api/v1/edit-sessions", {
      ...sessionResponse(),
      revision: 1,
      ops: [canonical],
    });
    http.json("POST", "/api/v1/edit-sessions/edit-1/commit", {
      ...sessionResponse(),
      revision: 2,
      outcome: "committed",
      hasUncommittedChanges: false,
    });
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });

    await act(async () => {
      await result.current.stageOps([local]);
    });
    expect(result.current.session?.ops).toEqual([canonical]);

    await act(async () => {
      await result.current.commit();
    });
    expect(http.count("POST", "/api/v1/edit-sessions/edit-1/stage")).toBe(0);
    expect(http.count("POST", "/api/v1/edit-sessions/edit-1/commit")).toBe(1);
  });

  it("restages a failed local operation before Save", async () => {
    http.json("POST", "/api/v1/edit-sessions", {
      ...sessionResponse(),
      revision: 1,
      ops: [setField("1")],
    });
    const stagePath = "/api/v1/edit-sessions/edit-1/stage";
    http.on("POST", stagePath, () => Promise.reject(new Error("offline")));
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });

    await act(async () => {
      await result.current.stageOps([setField("1")]);
      await expect(result.current.stageOps([setField("2")])).rejects.toThrow("offline");
    });
    expect(result.current.error).toContain("offline");

    http.on("POST", stagePath, () =>
      jsonReply({ ...sessionResponse(), revision: 2, ops: [setField("2")] }),
    );
    http.json("POST", "/api/v1/edit-sessions/edit-1/commit", {
      ...sessionResponse(),
      revision: 3,
      outcome: "committed",
      hasUncommittedChanges: false,
    });
    await act(async () => {
      await result.current.commit();
    });

    const flush = http.requests("POST", stagePath).at(-1);
    expect(JSON.parse(flush?.body || "null")).toMatchObject({
      replace: true,
      expectedRevision: 1,
      ops: [{ value: "2" }],
    });
    expect(result.current.session).toBeNull();
  });

  it("keeps edits typed while Save is in flight", async () => {
    http.json("POST", "/api/v1/edit-sessions", {
      ...sessionResponse(),
      revision: 1,
      ops: [{ ...setField("1"), id: "field-x" }],
    });
    const commit = deferredReply<OntologyEditSessionResponse>();
    http.on("POST", "/api/v1/edit-sessions/edit-1/commit", () => commit.promise);
    http.json("POST", "/api/v1/edit-sessions/edit-1/stage", {
      ...sessionResponse(),
      revision: 3,
      ops: [{ ...setField("2"), id: "field-x" }],
    });
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });
    await act(async () => {
      await result.current.stageOps([{ ...setField("1"), id: "field-x" }]);
    });

    let saving!: Promise<OntologyEditSessionResponse | null>;
    let newer!: Promise<OntologyEditSessionResponse | null>;
    act(() => {
      saving = result.current.commit();
      newer = result.current.stageOps([{ ...setField("2"), id: "field-x" }]);
    });
    await act(async () => {
      commit.resolve({
        ...sessionResponse(),
        revision: 2,
        outcome: "committed",
        hasUncommittedChanges: false,
      });
      await saving;
      await newer;
    });

    expect(result.current.session?.ops).toEqual([{ ...setField("2"), id: "field-x" }]);
    expect(result.current.session?.hasUncommittedChanges).toBe(true);
    expect(
      JSON.parse(
        http.requests("POST", "/api/v1/edit-sessions/edit-1/stage").at(-1)?.body || "null",
      ),
    ).toMatchObject({ expectedRevision: 2 });
  });

  it("does not forget an unacknowledged edit after a later stage succeeds", async () => {
    http.json("POST", "/api/v1/edit-sessions", {
      ...sessionResponse(),
      revision: 1,
      ops: [{ ...setField("seed"), id: "seed" }],
    });
    const stagePath = "/api/v1/edit-sessions/edit-1/stage";
    let stageAttempt = 0;
    http.on("POST", stagePath, () => {
      stageAttempt++;

      if (stageAttempt <= 2) return Promise.reject(new Error("lost acknowledgement"));

      return jsonReply({
        ...sessionResponse(),
        revision: 2,
        ops: [{ ...setField("b"), id: "field-b" }],
      });
    });
    http.json("POST", "/api/v1/edit-sessions/edit-1/commit", {
      ...sessionResponse(),
      revision: 4,
      outcome: "committed",
      hasUncommittedChanges: false,
    });
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });

    await act(async () => {
      await result.current.stageOps([{ ...setField("seed"), id: "seed" }]);
      await expect(result.current.stageOps([{ ...setField("a"), id: "field-a" }])).rejects.toThrow(
        "lost acknowledgement",
      );
      await result.current.stageOps([{ ...setField("b"), id: "field-b" }]);
      await result.current.commit();
    });

    const flush = http.requests("POST", stagePath).at(-1);
    expect(JSON.parse(flush?.body || "null")).toMatchObject({
      replace: true,
      ops: [{ id: "seed" }, { id: "field-a" }, { id: "field-b" }],
    });
    expect(result.current.session).toBeNull();
  });

  it("retries the exact committed submission after a lost response while preserving newer edits", async () => {
    http.json("POST", "/api/v1/edit-sessions", {
      ...sessionResponse(),
      revision: 1,
      ops: [{ ...setField("1"), id: "field-x" }],
    });
    const commitPath = "/api/v1/edit-sessions/edit-1/commit";
    let attempt = 0;
    http.on("POST", commitPath, () => {
      attempt++;

      if (attempt === 1) return Promise.reject(new Error("response lost"));

      return jsonReply({
        ...sessionResponse(),
        revision: 2,
        outcome: "committed",
        hasUncommittedChanges: false,
      });
    });
    http.json("POST", "/api/v1/edit-sessions/edit-1/stage", {
      ...sessionResponse(),
      revision: 3,
      ops: [{ ...setField("2"), id: "field-y", field: "y" }],
    });
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });
    await act(async () => {
      await result.current.stageOps([{ ...setField("1"), id: "field-x" }]);
      await result.current.commit();
      await result.current.stageOps([{ ...setField("2"), id: "field-y", field: "y" }]);
      await result.current.commit();
    });

    const requests = http
      .requests("POST", commitPath)
      .map((request) => JSON.parse(request.body || "null"));

    expect(requests).toHaveLength(2);
    expect(requests[0].requestId).toBe(requests[1].requestId);
    expect(requests[0]).toEqual(requests[1]);
    expect(requests[0].snapshot.ops).toEqual([
      expect.objectContaining({ id: "field-x", value: "1" }),
    ]);
    expect(result.current.session?.ops).toEqual([
      expect.objectContaining({ id: "field-y", value: "2" }),
    ]);
  });

  it("drops an edit the server refuses and keeps the rest savable", async () => {
    const kept = { ...setField("kept"), id: "field-kept" };
    const refused = { ...setField("refused"), id: "field-refused", field: "y" };
    http.json("POST", "/api/v1/edit-sessions", { ...sessionResponse(), revision: 1, ops: [kept] });
    http.on("POST", "/api/v1/edit-sessions/edit-1/stage", () =>
      jsonReply({ error: "missing node: structure changed", code: "BAD_REQUEST" }, 400),
    );
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });

    await act(async () => {
      await result.current.stageOps([kept]);
      await expect(result.current.stageOps([refused])).rejects.toThrow("structure changed");
    });

    expect(result.current.session?.ops).toEqual([kept]);
    expect(result.current.error).toBe(
      "Couldn't stage the change to y: missing node: structure changed",
    );

    http.json("POST", "/api/v1/edit-sessions/edit-1/commit", {
      ...sessionResponse(),
      revision: 2,
      outcome: "committed",
      hasUncommittedChanges: false,
    });
    await act(async () => {
      await result.current.commit();
    });
    // Nothing is left unacknowledged, so Save commits without restaging the refused edit.
    expect(http.count("POST", "/api/v1/edit-sessions/edit-1/stage")).toBe(2);
    expect(result.current.session).toBeNull();
  });

  it("keeps the first edit's target when the same field is edited again", async () => {
    const first = { ...setField("1"), id: "field-x" };
    http.json("POST", "/api/v1/edit-sessions", { ...sessionResponse(), revision: 1, ops: [first] });
    http.json("POST", "/api/v1/edit-sessions/edit-1/stage", {
      ...sessionResponse(),
      revision: 2,
      ops: [{ ...first, value: "2" }],
    });
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });

    await act(async () => {
      await result.current.stageOps([first]);
      // Read from staged state, the second edit carries the preview's identity.
      await result.current.stageOps([
        { ...setField("2"), id: "field-x", structuralFingerprint: "preview", nodeId: "preview" },
      ]);
    });

    const stage = JSON.parse(
      http.requests("POST", "/api/v1/edit-sessions/edit-1/stage")[0]?.body || "null",
    );

    expect(stage.ops[0]).toMatchObject({ value: "2" });
    expect(stage.ops[0].structuralFingerprint).toBeUndefined();
    expect(stage.ops[0].nodeId).toBeUndefined();
  });
});

it.each(["committed", "unchanged", undefined] as const)(
  "finishes saving (%s) and accepts the next edit while authoritative refetches remain pending",
  async (outcome) => {
    localStorage.clear();
    sessionStorage.clear();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    vi.spyOn(client, "invalidateQueries").mockReturnValue(new Promise<void>(() => {}));

    const Wrapper = ({ children }: PropsWithChildren) => (
      <QueryClientProvider client={client}>
        <OntologyEditSessionProvider vaultKey="/queue-test">{children}</OntologyEditSessionProvider>
      </QueryClientProvider>
    );

    http.json("POST", "/api/v1/edit-sessions", {
      ...sessionResponse(),
      revision: 1,
      ops: [setField("1")],
    });
    http.json("POST", "/api/v1/edit-sessions/edit-1/commit", {
      ...sessionResponse(),
      revision: 2,
      outcome,
      hasUncommittedChanges: false,
    });
    http.json("POST", "/api/v1/edit-sessions/edit-1/stage", {
      ...sessionResponse(),
      revision: 3,
      ops: [setField("2")],
    });
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper: Wrapper });
    await act(async () => {
      await result.current.stageOps([setField("1")]);
    });
    // WHY: awaiting commit inside act flushes React's render and the effect
    // that publishes result.current. Waiting on a plain flag and asserting
    // afterwards raced that flush under load. A commit that waited on the
    // never-settling invalidation would hang here and time out.
    await act(async () => {
      await result.current.commit();
    });
    expect(result.current.saving).toBe(false);
    expect(result.current.busy).toBe(false);
    expect(result.current.readLifecycle).toMatchObject({
      revision: 1,
      outcome: "saved",
      savedSession: { ops: [setField("1")] },
    });
    http.json("POST", "/api/v1/edit-sessions", {
      ...sessionResponse(),
      revision: 1,
      ops: [setField("2")],
    });
    await act(async () => {
      await result.current.stageOps([setField("2")]);
    });
    expect(result.current.session?.ops).toEqual([setField("2")]);
  },
);

it.each(["failed", "conflicted"] as const)(
  "retains edits when Save reports %s even if the response says clean",
  async (outcome) => {
    localStorage.clear();
    sessionStorage.clear();
    http.json("POST", "/api/v1/edit-sessions", {
      ...sessionResponse(),
      revision: 1,
      ops: [setField("1")],
    });
    http.json("POST", "/api/v1/edit-sessions/edit-1/commit", {
      ...sessionResponse(),
      revision: 2,
      outcome,
      status: outcome,
      ops: [setField("1")],
      hasUncommittedChanges: false,
    });
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });
    await act(async () => {
      await result.current.stageOps([setField("1")]);
      await result.current.commit();
    });
    expect(result.current.session?.ops).toEqual([setField("1")]);
    expect(result.current.readLifecycle).toMatchObject({
      revision: 0,
      outcome,
      savedSession: null,
    });
    expect(result.current.saving).toBe(false);
  },
);

it.each([false, true])(
  "remaps a stale embedded follow-up edit after Save (queued during save: %s)",
  async (queued) => {
    localStorage.clear();
    sessionStorage.clear();

    const original = {
      notePath: "notes/a.md",
      fragment: "item-100",
      nodeId: "notes/a.md#item-100",
      kind: "EMBEDDED",
      structuralFingerprint: "before-save",
    };

    // An earlier item also grew during the save, so both the offset and witness changed.
    const preview = {
      ...original,
      fragment: "item-130",
      nodeId: "notes/a.md#item-130",
      structuralFingerprint: "after-save",
    };

    const first: OntologyEditOp = {
      id: "status",
      kind: "setField",
      path: "notes/a.md#item-100",
      nodeId: original.nodeId,
      structuralFingerprint: original.structuralFingerprint,
      field: "status",
      value: "done",
      expected: { field: { kind: "scalar", scalar: "open" } },
    };

    const followup: OntologyEditOp = {
      ...first,
      value: "blocked",
      expected: { field: { kind: "scalar", scalar: "done" } },
    };

    const save = deferredReply<OntologyEditSessionResponse>();
    http.on("POST", "/api/v1/edit-sessions", (request) =>
      jsonReply({ ...sessionResponse(), revision: 1, ops: JSON.parse(request.body || "{}").ops }),
    );
    http.on("POST", "/api/v1/edit-sessions/edit-1/commit", () => save.promise);
    http.on("POST", "/api/v1/edit-sessions/edit-1/stage", (request) =>
      jsonReply({ ...sessionResponse(), revision: 3, ops: JSON.parse(request.body || "{}").ops }),
    );
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });
    await act(async () => {
      await result.current.stageOps([first]);
    });
    let committing!: Promise<OntologyEditSessionResponse | null>;
    let staging!: Promise<OntologyEditSessionResponse | null>;
    act(() => {
      committing = result.current.commit();

      if (queued) staging = result.current.stageOps([followup]);
    });
    await waitFor(() => expect(http.count("POST", "/api/v1/edit-sessions/edit-1/commit")).toBe(1));
    await act(async () => {
      save.resolve({
        ...sessionResponse(),
        revision: 2,
        outcome: "committed",
        hasUncommittedChanges: false,
        refLineage: [{ original, preview }],
      });
      await committing;

      if (queued) await staging;
      else
        await result.current.stageOps(remapCommittedEditOps([followup], [{ original, preview }]));
    });
    const path = queued ? "/api/v1/edit-sessions/edit-1/stage" : "/api/v1/edit-sessions";
    const submitted = JSON.parse(http.requests("POST", path).at(-1)?.body || "{}").ops[0];
    expect(submitted).toEqual({
      ...followup,
      path: "notes/a.md#item-130",
      nodeId: preview.nodeId,
      structuralFingerprint: preview.structuralFingerprint,
    });
    expect(result.current.session?.ops).toEqual([submitted]);
  },
);

it.each([false, true])(
  "maps only held reader refs after canonical adoption (discarded: %s)",
  async (discarded) => {
    localStorage.clear();
    sessionStorage.clear();

    const original = {
      notePath: "notes/a.md",
      fragment: "item-100",
      nodeId: "notes/a.md#item-100",
      kind: "EMBEDDED",
      structuralFingerprint: "original-source",
    };

    const preview = { ...original, structuralFingerprint: "saved-source" };
    const lineage = [{ original, preview }];

    const op: OntologyEditOp = {
      id: "status",
      kind: "setField",
      path: "notes/a.md#item-100",
      nodeId: original.nodeId,
      structuralFingerprint: original.structuralFingerprint,
      field: "status",
      value: "done",
    };

    http.on("POST", "/api/v1/edit-sessions", (request) =>
      jsonReply({
        ...sessionResponse(),
        revision: 1,
        ops: JSON.parse(request.body || "{}").ops,
      }),
    );
    http.on("POST", "/api/v1/edit-sessions/edit-1/stage", (request) =>
      jsonReply({
        ...sessionResponse(),
        revision: 3,
        ops: JSON.parse(request.body || "{}").ops,
      }),
    );
    http.json("POST", "/api/v1/edit-sessions/edit-1/commit", {
      ...sessionResponse(),
      revision: 2,
      outcome: "committed",
      hasUncommittedChanges: false,
      refLineage: lineage,
    });
    const { result } = renderHook(() => useOntologyEditSession(), { wrapper });
    await act(async () => {
      await result.current.stageOps([op]);
      await result.current.commit();

      if (discarded) await result.current.discard();
      // Reader one adopted a fresh query after an external restore. Reader two
      // still holds its saved display snapshot and supplies only that lineage.
      await result.current.stageOps([{ ...op, id: "fresh-reader", value: "fresh edit" }]);
      await result.current.stageOps(
        remapCommittedEditOps([{ ...op, id: "held-reader", value: "held edit" }], lineage),
      );
    });

    const fresh = JSON.parse(http.requests("POST", "/api/v1/edit-sessions").at(-1)?.body || "{}")
      .ops[0];

    expect(fresh.structuralFingerprint).toBe(original.structuralFingerprint);

    const held = JSON.parse(
      http.requests("POST", "/api/v1/edit-sessions/edit-1/stage").at(-1)?.body || "{}",
    ).ops[0];

    expect(held.structuralFingerprint).toBe(preview.structuralFingerprint);
  },
);
