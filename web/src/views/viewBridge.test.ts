import { describe, expect, it, vi } from "vitest";

import {
  FRAME_READY_MESSAGE,
  LOCATION_ACCEPTED_MESSAGE,
  LOCATION_RENDERED_MESSAGE,
  OPEN_SEARCH_MESSAGE,
  PHASE_MESSAGE,
  STAGE_OPS_MESSAGE,
  STAGE_RESULT_MESSAGE,
} from "../lib/customViewMessages";
import {
  isServiceRequest,
  messageStamp,
  runServiceRequest,
  viewRequest,
  type BridgeServices,
} from "./viewBridge";

const quiet = () => vi.spyOn(console, "warn").mockImplementation(() => undefined);

const services = (overrides: Partial<BridgeServices> = {}): BridgeServices => ({
  session: null,
  onOpenNote: vi.fn(),
  onStageOps: vi.fn(async () => undefined),
  ...overrides,
});

describe("viewRequest", () => {
  it("opens a search by query alone, by folder alone, or both, and rejects neither", () => {
    quiet();

    expect(viewRequest({ type: OPEN_SEARCH_MESSAGE, query: "plan" }, [])).toEqual({
      kind: "open-search",
      search: { query: "plan" },
    });
    expect(viewRequest({ type: OPEN_SEARCH_MESSAGE, folder: "Notes" }, [])).toEqual({
      kind: "open-search",
      search: { folder: "Notes" },
    });
    expect(viewRequest({ type: OPEN_SEARCH_MESSAGE }, [])).toBeNull();
    expect(viewRequest({ type: OPEN_SEARCH_MESSAGE, query: " " }, [])).toBeNull();
    expect(viewRequest({ type: OPEN_SEARCH_MESSAGE, folder: "" }, [])).toBeNull();
  });

  it("reads the experience frame's own messages and their page stamps", () => {
    quiet();
    const stamp = { tabId: "note:a.md", navigationId: "n1" };

    expect(viewRequest({ type: FRAME_READY_MESSAGE }, [])).toEqual({ kind: "frame-ready" });
    expect(viewRequest({ type: LOCATION_ACCEPTED_MESSAGE, ...stamp }, [])).toEqual({
      kind: "location-accepted",
      ...stamp,
    });
    expect(viewRequest({ type: LOCATION_RENDERED_MESSAGE, ...stamp, at: 12 }, [])).toEqual({
      kind: "location-rendered",
      ...stamp,
      at: 12,
    });
    expect(viewRequest({ type: LOCATION_ACCEPTED_MESSAGE, tabId: "x" }, [])).toBeNull();
    expect(viewRequest({ type: PHASE_MESSAGE, phase: "first-render", at: 3 }, [])).toEqual({
      kind: "phase",
      phase: "first-render",
      at: 3,
    });
    expect(viewRequest({ type: PHASE_MESSAGE, phase: "painted", at: 3 }, [])).toBeNull();

    expect(messageStamp({ type: STAGE_OPS_MESSAGE, ...stamp })).toEqual(stamp);
    expect(messageStamp({ type: STAGE_OPS_MESSAGE, tabId: 7 })).toEqual({});
  });
});

describe("runServiceRequest", () => {
  it("answers every stage request, with the error when staging fails", async () => {
    const reply = vi.fn();
    const failing = services({ onStageOps: vi.fn(async () => Promise.reject(new Error("no"))) });

    const request = viewRequest(
      { type: STAGE_OPS_MESSAGE, requestId: "r1", ops: [{ op: "x" }] },
      [],
    );

    expect(request && isServiceRequest(request)).toBe(true);

    if (!request || !isServiceRequest(request)) return;
    runServiceRequest(request, services(), reply);
    runServiceRequest(request, failing, reply);
    await vi.waitFor(() => expect(reply).toHaveBeenCalledTimes(2));

    expect(reply.mock.calls).toEqual(
      expect.arrayContaining([
        [{ type: STAGE_RESULT_MESSAGE, requestId: "r1" }],
        [{ type: STAGE_RESULT_MESSAGE, requestId: "r1", error: "no" }],
      ]),
    );
  });
});
