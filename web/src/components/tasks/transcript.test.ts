import { describe, expect, it } from "vitest";
import type { TaskTranscriptEvent } from "../../api/tasks";
import { appendEvents, buildTimeline } from "./transcript";

function line(id: number, type: string, content: string): TaskTranscriptEvent {
  return { id, sessionId: "run", type, content, createdAt: "2026-10-31T12:00:00Z" };
}

describe("transcript lines", () => {
  it("lets a repeated id replace the held line, within one read and across reads", () => {
    const first = appendEvents([], [line(1, "reasoning", "partial"), line(2, "reasoning", "two")]);

    const corrected = appendEvents(first, [line(1, "reasoning", "whole thought")]);

    expect(corrected.map((event) => [event.id, event.content])).toEqual([
      [1, "whole thought"],
      [2, "two"],
    ]);

    const once = appendEvents([], [line(1, "reasoning", "a"), line(1, "reasoning", "b")]);

    expect(once).toHaveLength(1);
  });

  it("keeps separate thinking blocks that have no item id", () => {
    const blocks = buildTimeline([line(1, "reasoning", "first"), line(2, "reasoning", "second")]);

    expect(blocks.map((block) => block.kind)).toEqual(["thinking", "thinking"]);
  });

  it("drops the streamed copy of the final answer pinned above the timeline", () => {
    const blocks = buildTimeline([
      line(1, "assistant-text-delta", "Done: three notes fixed."),
      line(2, "final-message", "Done: three notes fixed."),
    ]);

    expect(blocks).toEqual([]);
  });
});
