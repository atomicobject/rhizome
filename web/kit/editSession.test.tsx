import { afterEach, describe, expect, it, vi } from "vitest";

import type { OntologyEditOp } from "../src/api/types";
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
});
