import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { describe, expect, it } from "vitest";

import type { OntologyEditOp, OntologyEditSessionResponse } from "../api/types";
import { remapCommittedEditOps } from "../components/editing/editSessionState";
import {
  editSessionFingerprint,
  forgetCommittedStagedReads,
  stagedQueryKey,
  useStagedQuery,
} from "./stagedQuery";

type SessionProps = { session: OntologyEditSessionResponse | null };

function wrapper({ children }: PropsWithChildren) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("useStagedQuery", () => {
  it("keeps the same subject on screen while a refinement refetches, but not another subject", async () => {
    const pending = new Map<string, () => void>();

    const { result, rerender } = renderHook(
      ({ subject, refinement }: { subject: string; refinement: number }) =>
        useStagedQuery({
          subject: ["test", subject],
          refinement,
          session: null,
          queryFn: () =>
            new Promise<string>((resolve) => {
              pending.set(`${subject}:${refinement}`, () => resolve(`${subject}:${refinement}`));
            }),
        }),
      { wrapper, initialProps: { subject: "a", refinement: 1 } },
    );

    await waitFor(() => expect(pending.has("a:1")).toBe(true));
    pending.get("a:1")?.();
    await waitFor(() => expect(result.current.data).toBe("a:1"));

    rerender({ subject: "a", refinement: 2 });
    expect(result.current.data).toBe("a:1");
    expect(result.current.isPlaceholderData).toBe(true);

    rerender({ subject: "b", refinement: 2 });
    expect(result.current.data).toBeUndefined();
  });

  it("re-keys reads only when the server acknowledges the session", () => {
    const local: OntologyEditSessionResponse = {
      sessionId: "s1",
      status: "dirty",
      hasUncommittedChanges: true,
      createdAt: "2026-09-23T00:00:00Z",
      updatedAt: "2026-09-23T00:00:01Z",
    };

    const acknowledged = { ...local, revision: 1 };

    expect(editSessionFingerprint(local)).toBe(editSessionFingerprint(null));
    expect(editSessionFingerprint({ ...acknowledged, updatedAt: "2026-09-23T00:00:09Z" })).toBe(
      editSessionFingerprint(acknowledged),
    );
    expect(editSessionFingerprint({ ...acknowledged, revision: 2 })).not.toBe(
      editSessionFingerprint(acknowledged),
    );
  });

  it("holds the staged result after a save instead of pre-save committed data", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(stagedQueryKey(["test", "a"], null, null), "before edit");

    const saved: OntologyEditSessionResponse = {
      sessionId: "s1",
      status: "dirty",
      revision: 1,
      hasUncommittedChanges: true,
      createdAt: "2026-09-23T00:00:00Z",
      updatedAt: "2026-09-23T00:00:01Z",
    };

    const initialProps: SessionProps = { session: saved };

    const { result, rerender } = renderHook(
      ({ session }: SessionProps) =>
        useStagedQuery({
          subject: ["test", "a"],
          session,
          queryFn: ({ editSession }) =>
            editSession ? Promise.resolve("staged") : new Promise<string>(() => {}),
        }),
      {
        initialProps,
        wrapper: ({ children }: PropsWithChildren) => (
          <QueryClientProvider client={client}>{children}</QueryClientProvider>
        ),
      },
    );

    await waitFor(() => expect(result.current.data).toBe("staged"));
    forgetCommittedStagedReads(client);
    rerender({ session: null });
    expect(result.current.data).toBe("staged");
  });
});

describe("saved display edits", () => {
  const saved: OntologyEditSessionResponse = {
    sessionId: "s1",
    revision: 1,
    status: "dirty",
    hasUncommittedChanges: true,
    createdAt: "2026-10-02T00:00:00Z",
    updatedAt: "2026-10-02T00:00:00Z",
    ops: [{ kind: "setField", path: "a.md", field: "rank", value: "4" }],
  };

  it("holds saved operations through late staged responses, read failure, and rapid saves", async () => {
    type Props = {
      session: typeof saved | null;
      lifecycle: import("./stagedQuery").EditReadLifecycle;
    };

    const pending: {
      resolve: (value: string) => void;
      reject: (error: Error) => void;
      signal: AbortSignal;
    }[] = [];

    const initialProps: Props = {
      session: null,
      lifecycle: { revision: 0, outcome: null, savedSession: null },
    };

    const { result, rerender } = renderHook(
      ({ session, lifecycle }: Props) =>
        useStagedQuery({
          subject: ["save-race"],
          session,
          lifecycle,
          queryFn: ({ signal }) =>
            new Promise<string>((resolve, reject) => pending.push({ resolve, reject, signal })),
        }),
      { wrapper, initialProps },
    );

    await waitFor(() => expect(pending).toHaveLength(1));
    pending[0].resolve("old rows");
    await waitFor(() => expect(result.current.data).toBe("old rows"));

    rerender({ session: saved, lifecycle: initialProps.lifecycle });
    await waitFor(() => expect(pending).toHaveLength(2));
    rerender({ session: null, lifecycle: { revision: 1, outcome: "saved", savedSession: saved } });
    await waitFor(() => expect(pending).toHaveLength(3));
    expect(pending[1].signal.aborted).toBe(true);
    pending[1].resolve("late staged rows");
    expect(result.current.displaySession?.ops).toEqual(saved.ops);
    expect(result.current.displayData).toBe("old rows");

    const newer = {
      ...saved,
      ops: [{ kind: "setField", path: "b.md", field: "rank", value: "5" }],
    };

    rerender({ session: null, lifecycle: { revision: 2, outcome: "saved", savedSession: newer } });
    await waitFor(() => expect(pending).toHaveLength(4));
    pending[2].resolve("first save rows");
    pending[3].reject(new Error("refresh failed"));
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.displayData).toBe("old rows");
    expect(result.current.displaySession?.ops).toEqual([...saved.ops!, ...newer.ops]);

    void result.current.refetch();
    await waitFor(() => expect(pending).toHaveLength(5));
    pending[4].resolve("canonical rows");
    await waitFor(() => expect(result.current.displayData).toBe("canonical rows"));
    expect(result.current.savedEditsPending).toBe(false);
    expect(result.current.displaySession).toBeNull();

    rerender({ session: newer, lifecycle: { revision: 2, outcome: "saved", savedSession: newer } });
    expect(result.current.displaySession?.ops).toEqual(newer.ops);
    expect(result.current.savedEditsPending).toBe(false);
    rerender({
      session: null,
      lifecycle: { revision: 3, outcome: "discarded", savedSession: null },
    });
    expect(result.current.displaySession).toBeNull();
  });
});

it("does not retire saved edits from initialData before a canonical request returns", async () => {
  const saved: OntologyEditSessionResponse = {
    sessionId: "initial-data",
    revision: 1,
    status: "dirty",
    hasUncommittedChanges: true,
    createdAt: "2026-10-02T00:00:00Z",
    updatedAt: "2026-10-02T00:00:00Z",
    ops: [{ kind: "setField", path: "a.md", field: "rank", value: "4" }],
  };

  let canonical!: (value: string) => void;

  const { result } = renderHook(
    () =>
      useStagedQuery({
        subject: ["initial-data-save"],
        session: null,
        lifecycle: { revision: 1, outcome: "saved", savedSession: saved },
        options: { initialData: "old rows", staleTime: Infinity },
        queryFn: () =>
          new Promise<string>((resolve) => {
            canonical = resolve;
          }),
      }),
    { wrapper },
  );

  expect(result.current.data).toBe("old rows");
  expect(result.current.isSuccess).toBe(true);
  expect(result.current.savedEditsPending).toBe(true);
  expect(result.current.displaySession?.ops).toEqual(saved.ops);
  canonical("canonical rows");
  await waitFor(() => expect(result.current.data).toBe("canonical rows"));
  expect(result.current.savedEditsPending).toBe(false);
});

it("discards only newer dirty values while saved edits still await their canonical read", async () => {
  const saved: OntologyEditSessionResponse = {
    sessionId: "discard-newer",
    revision: 1,
    status: "dirty",
    hasUncommittedChanges: true,
    createdAt: "2026-10-02T00:00:00Z",
    updatedAt: "2026-10-02T00:00:00Z",
    ops: [{ kind: "setField", path: "a.md", field: "rank", value: "4" }],
  };

  type Props = {
    session: typeof saved | null;
    lifecycle: import("./stagedQuery").EditReadLifecycle;
  };

  const pending: ((value: string) => void)[] = [];

  const initialProps: Props = {
    session: null,
    lifecycle: { revision: 1, outcome: "saved", savedSession: saved },
  };

  const { result, rerender } = renderHook(
    ({ session, lifecycle }: Props) =>
      useStagedQuery({
        subject: ["discard-newer"],
        session,
        lifecycle,
        options: { initialData: "old raw rows" },
        queryFn: () => new Promise<string>((resolve) => pending.push(resolve)),
      }),
    { wrapper, initialProps },
  );

  expect(result.current.displaySession?.ops?.at(-1)?.value).toBe("4");
  rerender({
    ...initialProps,
    session: { ...saved, revision: 2, ops: [{ ...saved.ops![0], value: "5" }] },
  });
  expect(result.current.displaySession?.ops?.at(-1)?.value).toBe("5");
  rerender({ session: null, lifecycle: { revision: 2, outcome: "discarded", savedSession: null } });
  expect(result.current.displaySession?.ops?.at(-1)?.value).toBe("4");
  expect(result.current.savedEditsPending).toBe(true);
  expect(result.current.displayData).toBe("old raw rows");
  pending.at(-1)?.("saved canonical rows");
  await waitFor(() => expect(result.current.displayData).toBe("saved canonical rows"));
  expect(result.current.displaySession).toBeNull();
});

it("retires saved ref aliases per reader and preserves another held reader through discard", async () => {
  const original = {
    notePath: "a.md",
    fragment: "item-10",
    nodeId: "a.md#item-10",
    kind: "EMBEDDED",
    structuralFingerprint: "original",
  };

  const preview = { ...original, structuralFingerprint: "saved" };

  const op: OntologyEditOp = {
    kind: "setField",
    path: "a.md#item-10",
    nodeId: original.nodeId,
    structuralFingerprint: original.structuralFingerprint,
    field: "status",
    value: "done",
  };

  const saved: OntologyEditSessionResponse = {
    sessionId: "two-readers",
    revision: 1,
    status: "clean",
    hasUncommittedChanges: false,
    createdAt: "2026-10-02T00:00:00Z",
    updatedAt: "2026-10-02T00:00:00Z",
    ops: [op],
    refLineage: [{ original, preview }],
  };

  type Lifecycle = import("./stagedQuery").EditReadLifecycle;

  const pending = new Map<string, (value: typeof original) => void>();
  const initialProps: Lifecycle = { revision: 1, outcome: "saved", savedSession: saved };

  const { result, rerender } = renderHook(
    (lifecycle: Lifecycle) => {
      const fast = useStagedQuery({
        subject: ["aliases", "fast"],
        session: null,
        lifecycle,
        options: { initialData: original },
        queryFn: () => new Promise<typeof original>((resolve) => pending.set("fast", resolve)),
      });

      const slow = useStagedQuery({
        subject: ["aliases", "slow"],
        session: null,
        lifecycle,
        options: { initialData: original },
        queryFn: () => new Promise<typeof original>((resolve) => pending.set("slow", resolve)),
      });

      return { fast, slow };
    },
    { wrapper, initialProps },
  );

  pending.get("fast")?.(original);
  await waitFor(() => expect(result.current.fast.savedEditsPending).toBe(false));
  expect(
    remapCommittedEditOps([op], result.current.fast.displaySession?.refLineage ?? [])[0],
  ).toEqual(op);
  expect(result.current.slow.savedEditsPending).toBe(true);
  expect(
    remapCommittedEditOps([op], result.current.slow.displaySession?.refLineage ?? [])[0]
      .structuralFingerprint,
  ).toBe("saved");
  rerender({ revision: 2, outcome: "discarded", savedSession: null });
  expect(result.current.fast.displaySession).toBeNull();
  expect(result.current.slow.displaySession?.refLineage).toEqual(saved.refLineage);
  pending.get("slow")?.(original);
  await waitFor(() => expect(result.current.slow.savedEditsPending).toBe(false));
  expect(result.current.slow.displaySession).toBeNull();
});
