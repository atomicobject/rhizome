import { afterEach, describe, expect, it, vi } from "vitest";

import type { OntologyEditOp, OntologyEditSessionResponse } from "../src/api/types";
import {
  EDIT_SESSION_HELLO_MESSAGE,
  EDIT_SESSION_MESSAGE,
  STAGE_OPS_MESSAGE,
  STAGE_RESULT_MESSAGE,
  type HostMessage,
} from "../src/lib/customViewMessages";
import { connectWorkspaceHost } from "./editSession";

const op: OntologyEditOp = { kind: "setField", path: "docs/a.md#item-1", field: "done" };

function fakeParent() {
  const frame = document.createElement("iframe");
  document.body.append(frame);
  const parent = frame.contentWindow!;
  const sent = vi.spyOn(parent, "postMessage");

  const answer = (data: HostMessage, source: Window = parent) =>
    window.dispatchEvent(
      new MessageEvent("message", { source, origin: window.location.origin, data }),
    );

  return { parent, sent, answer, types: () => sent.mock.calls.map(([message]) => message.type) };
}

afterEach(() => {
  vi.useRealTimers();
  document.body.replaceChildren();
});

describe("connectWorkspaceHost", () => {
  it("holds a stage until the host answers, then relays its result", async () => {
    const { parent, sent, answer, types } = fakeParent();
    const host = connectWorkspaceHost(parent);
    const staged = host.stage([op]);
    await Promise.resolve();

    expect(types()).not.toContain(STAGE_OPS_MESSAGE);

    answer({ type: EDIT_SESSION_MESSAGE, session: null }, window);
    await Promise.resolve();
    expect(types()).not.toContain(STAGE_OPS_MESSAGE);

    answer({ type: EDIT_SESSION_MESSAGE, session: null });
    await vi.waitFor(() => expect(types()).toContain(STAGE_OPS_MESSAGE));
    expect(host.state().answered).toBe(true);

    const [request] = sent.mock.calls.find(([message]) => message.type === STAGE_OPS_MESSAGE)!;
    expect(request).toEqual({ type: STAGE_OPS_MESSAGE, requestId: "stage-1", ops: [op] });

    answer({ type: STAGE_RESULT_MESSAGE, requestId: "stage-1" });
    await expect(staged).resolves.toBeUndefined();

    const failed = host.stage([op]);
    await vi.waitFor(() =>
      expect(types().filter((type) => type === STAGE_OPS_MESSAGE)).toHaveLength(2),
    );
    answer({ type: STAGE_RESULT_MESSAGE, requestId: "stage-2", error: "note is reserved" });
    await expect(failed).rejects.toThrow("note is reserved");
  });

  it("fails a write instead of committing it when the host never answers", async () => {
    vi.useFakeTimers();
    const { parent, types } = fakeParent();
    const staged = connectWorkspaceHost(parent, 1_000).stage([op]);
    const outcome = expect(staged).rejects.toThrow(/did not answer/);

    await vi.advanceTimersByTimeAsync(1_000);
    await outcome;
    expect(types()).toEqual([EDIT_SESSION_HELLO_MESSAGE, EDIT_SESSION_HELLO_MESSAGE]);
  });

  it("stamps a page's stage with its navigation and surfaces the host's refusal of a stale one", async () => {
    const { parent, sent, answer } = fakeParent();
    const host = connectWorkspaceHost(parent);
    answer({ type: EDIT_SESSION_MESSAGE, session: null });
    const staged = host.stage([op], { tabId: "tab-1", navigationId: "n-1" });

    await vi.waitFor(() =>
      expect(sent.mock.calls.map(([message]) => message)).toContainEqual({
        type: STAGE_OPS_MESSAGE,
        requestId: "stage-1",
        ops: [op],
        tabId: "tab-1",
        navigationId: "n-1",
      }),
    );
    answer({ type: STAGE_RESULT_MESSAGE, requestId: "stage-1", error: "page navigated away" });
    await expect(staged).rejects.toThrow("page navigated away");
  });
});

describe("an edit session the host sends again", () => {
  const session: OntologyEditSessionResponse = {
    sessionId: "s-1",
    revision: 2,
    status: "dirty",
    hasUncommittedChanges: true,
    createdAt: "2026-11-09T00:00:00Z",
    updatedAt: "2026-11-09T00:00:00Z",
    ops: [op],
  };

  it("changes nothing when it is identical, as after a resume", () => {
    const { parent, answer } = fakeParent();
    const host = connectWorkspaceHost(parent);
    const listener = vi.fn();
    host.subscribe(listener);

    answer({ type: EDIT_SESSION_MESSAGE, session });
    const first = host.state();
    answer({ type: EDIT_SESSION_MESSAGE, session: { ...session, ops: [op] } });

    expect(listener).toHaveBeenCalledTimes(1);
    expect(host.state()).toBe(first);

    answer({ type: EDIT_SESSION_MESSAGE, session: { ...session, revision: 3 } });
    expect(listener).toHaveBeenCalledTimes(2);
    expect(host.state().session?.revision).toBe(3);
  });
});
