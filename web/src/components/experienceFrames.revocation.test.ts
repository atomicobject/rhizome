import { afterEach, expect, it, vi } from "vitest";
import { ExperienceFrames } from "./experienceFrames";
import type { JsonObject } from "../api/parse";
import { ApiError } from "../api/client";
import {
  FRAME_READY_MESSAGE,
  LOCATION_MESSAGE,
  OPEN_NOTE_MESSAGE,
} from "../lib/customViewMessages";

afterEach(() => {
  vi.useRealTimers();
  document.body.replaceChildren();
});

const showing = (id: string) => ({
  tabId: id,
  experienceKey: id,
  viewId: id,
  viewName: id,
  declarations: [],
  context: { kind: "group" as const, group: id },
  params: {},
  services: { onOpenNote() {}, async onStageOps() {} },
  ownsLocation: true,
});

const invocation = (id: string) => ({
  invocation: { check: `/views/_check/${id}/page`, view: { id } },
  generation: "g1",
});

it("destroys a repository frame whose invocation is in flight when trust is revoked", async () => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  let finish!: (value: ReturnType<typeof invocation>) => void;

  const pending = new Promise<ReturnType<typeof invocation>>((resolve) => {
    finish = resolve;
  });

  const refused = new ApiError("Repository views are not trusted.", {
    status: 403,
    url: "/api/v1/views/a/invocation",
    code: "VIEWS_UNTRUSTED",
  });

  const fetchInvocation = vi
    .fn()
    .mockResolvedValueOnce({
      ...invocation("bundled"),
      invocation: { check: "/views/_bundled/_check/home" },
    })
    .mockResolvedValueOnce(invocation("a"))
    .mockReturnValueOnce(pending)
    .mockRejectedValue(refused);

  const frames = new ExperienceFrames({ fetchInvocation, fetchGeneration: async () => "g1" });
  const windows = new Map<string, { target: Window; sent: unknown[] }>();
  frames.subscribe(() => {
    for (const view of frames.views()) {
      if (windows.has(view.experienceKey)) continue;
      const element = document.createElement("iframe");
      document.body.append(element);
      const target = element.contentWindow!;
      const sent: unknown[] = [];
      vi.spyOn(target, "postMessage").mockImplementation((value) => sent.push(value));
      windows.set(view.experienceKey, { target, sent });
      frames.attach(view.id, target);
    }
  });

  const from = (id: string, data: JsonObject) =>
    frames.receive(
      new MessageEvent("message", {
        origin: window.location.origin,
        source: windows.get(id)!.target,
        data,
      }),
    );

  const onOpenNote = vi.fn();
  frames.show(showing("bundled"), null);
  await vi.advanceTimersByTimeAsync(0);
  frames.show(showing("a"), null);
  await vi.advanceTimersByTimeAsync(0);
  frames.hide("a");
  frames.show(
    {
      ...showing("b"),
      panel: true,
      tabId: "panel",
      services: { onOpenNote, async onStageOps() {} },
    },
    null,
  );
  await vi.advanceTimersByTimeAsync(0);
  from("b", { type: FRAME_READY_MESSAGE });
  frames.show(showing("a"), null);
  await vi.advanceTimersByTimeAsync(0);
  expect(frames.views().map((view) => view.experienceKey)).toEqual(["bundled"]);
  finish(invocation("b"));
  await vi.advanceTimersByTimeAsync(0);
  expect(windows.get("b")!.sent).not.toContainEqual(
    expect.objectContaining({ type: LOCATION_MESSAGE }),
  );
  from("b", { type: OPEN_NOTE_MESSAGE, path: "a.md", tabId: "panel", navigationId: "n3" });
  expect(onOpenNote).not.toHaveBeenCalled();
  expect(frames.views().map((view) => view.experienceKey)).toEqual(["bundled"]);
  const calls = fetchInvocation.mock.calls.length;
  frames.show(showing("b"), null);
  await vi.advanceTimersByTimeAsync(0);
  expect(fetchInvocation).toHaveBeenCalledTimes(calls + 1);
});
