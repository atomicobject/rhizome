// An experience frame's error pages: the host reveals a frame only on its
// location-rendered message, so an error page must send one too.
import { act, render, screen } from "@testing-library/react";
import type { ComponentType } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import {
  LOCATION_MESSAGE,
  LOCATION_RENDERED_MESSAGE,
  type ViewMessage,
} from "../src/lib/customViewMessages";
import type { ViewContext } from "../src/views/context";
import { ExperiencePages, createExperienceFrame } from "./experienceFrame";
import { createFrameGeneration } from "./generation";
import type { ViewModule } from "./startView";
import { useViewContext } from "./viewContext";

let posted: ViewMessage[];

let stops: Array<() => void>;

beforeEach(() => {
  posted = [];
  stops = [];
  vi.spyOn(window, "postMessage").mockImplementation((message: ViewMessage) => {
    posted.push(message);
  });
  vi.spyOn(console, "error").mockImplementation(() => undefined);
});

afterEach(() => {
  for (const stop of stops) stop();
});

function start(modules: Record<string, () => Promise<ViewModule>>) {
  const generation = createFrameGeneration();

  const frame = createExperienceFrame({
    shellView: { id: "shell", name: "Shell" },
    shellLoad: async () => ({ default: () => null }),
    loadFailureDetail: async (error) => error.message,
    generation,
    importModule: (entry) => modules[entry](),
  });

  stops.push(frame.stop, generation.stop);
  render(<ExperiencePages frame={frame} />);
}

const loaded = (View: ComponentType) => async (): Promise<ViewModule> => ({
  default: () => <View />,
});

const locate = (tabId: string, navigationId: string, view: string, notePath = "a.md") =>
  act(async () => {
    const context: ViewContext = { kind: "node", type: "Spec", ref: { notePath, kind: "NOTE" } };

    window.dispatchEvent(
      new MessageEvent("message", {
        source: window,
        origin: window.location.origin,
        data: {
          type: LOCATION_MESSAGE,
          tabId,
          navigationId,
          params: {},
          invocation: {
            view: { id: view, name: view, origin: "repository", experienceKey: "x" },
            context,
            configuration: {},
            params: [],
            vaultKey: "test-vault",
            entry: view,
          },
        },
      }),
    );
  });

const painted = () => act(() => new Promise((resolve) => requestAnimationFrame(resolve)));

const rendered = () =>
  posted.flatMap((message) =>
    message.type === LOCATION_RENDERED_MESSAGE ? [message.navigationId] : [],
  );

function Fragile() {
  const context = useViewContext();

  if (context.kind === "node" && context.ref.notePath === "broken.md") throw new Error("boom");

  return <p>{context.kind === "node" ? context.ref.notePath : ""}</p>;
}

it("reports a page whose module import fails as rendered", async () => {
  start({ missing: () => Promise.reject(new Error("no such module")) });
  await locate("tab-1", "n-1", "missing");
  expect((await screen.findByRole("alert")).textContent).toContain("no such module");
  await painted();

  expect(posted).toContainEqual(
    expect.objectContaining({
      type: LOCATION_RENDERED_MESSAGE,
      tabId: "tab-1",
      at: expect.any(Number),
    }),
  );
  expect(rendered()).toEqual(["n-1"]);
});

it("reports a page that throws while rendering as rendered, and its replacement as usual", async () => {
  start({ record: loaded(Fragile) });
  await locate("tab-1", "n-1", "record", "broken.md");
  expect((await screen.findByRole("alert")).textContent).toContain("boom");
  await painted();
  expect(rendered()).toEqual(["n-1"]);

  // Another location for the same error page is a new navigation of it.
  await locate("tab-1", "n-2", "record", "broken.md");
  await painted();
  expect(rendered()).toEqual(["n-1", "n-2"]);

  await locate("tab-1", "n-3", "record", "fine.md");
  await painted();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByText("fine.md")).toBeTruthy();
  expect(rendered()).toEqual(["n-1", "n-2", "n-3"]);
});

it("never reports a stale navigation's failed import", async () => {
  let fail: (error: Error) => void = () => undefined;
  const slow = new Promise<ViewModule>((_, reject) => (fail = reject));

  start({ slow: () => slow, record: loaded(Fragile) });
  await locate("tab-1", "n-1", "slow");
  await locate("tab-1", "n-2", "record", "fine.md");
  await act(async () => fail(new Error("too late")));
  await painted();

  expect(screen.queryByRole("alert")).toBeNull();
  expect(rendered()).toEqual(["n-2"]);
});
