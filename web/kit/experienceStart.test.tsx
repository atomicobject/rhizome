// How an experience frame starts (customViewMessages.ts): the shell opts in
// with frame=experience, the kit says when it listens, and only a frame
// without a generation takes one from a location.
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  FRAME_READY_MESSAGE,
  LOCATION_ACCEPTED_MESSAGE,
  LOCATION_MESSAGE,
  VIEW_RENDERED_MESSAGE,
  type HostMessage,
  type ViewMessage,
} from "../src/lib/customViewMessages";
import { createExperienceFrame } from "./experienceFrame";
import { createFrameGeneration } from "./generation";
import { startView } from "./startView";

const parentDescriptor = Object.getOwnPropertyDescriptor(window, "parent")!;

let stops: Array<() => void>;

beforeEach(() => {
  stops = [];
});

afterEach(() => {
  for (const stop of stops) stop();
  Object.defineProperty(window, "parent", parentDescriptor);
  document.getElementById("rhizome-view-invocation")?.remove();
  window.history.replaceState(null, "", "/");
  delete document.documentElement.dataset.rhizomeView;
  vi.restoreAllMocks();
});

const location = (tabId: string, generation?: string): HostMessage => ({
  type: LOCATION_MESSAGE,
  tabId,
  navigationId: `n-${tabId}`,
  params: {},
  generation,
  invocation: {
    view: { id: "a", name: "A", experienceKey: "x" },
    context: { kind: "standalone" },
    entry: "a",
  },
});

const fromHost = (data: HostMessage) =>
  window.dispatchEvent(
    new MessageEvent("message", { source: window, origin: window.location.origin, data }),
  );

function frame(generation = createFrameGeneration()) {
  const created = createExperienceFrame({
    shellView: { id: "shell", name: "Shell" },
    shellLoad: async () => ({ default: () => null }),
    loadFailureDetail: async (error) => error.message,
    generation,
    importModule: async () => ({ default: () => null }),
  });

  stops.push(created.stop, generation.stop);

  return created;
}

describe("the ready signal", () => {
  it("is posted once the frame listens, so a location sent in answer is accepted", () => {
    const posted: ViewMessage[] = [];

    vi.spyOn(window, "postMessage").mockImplementation((message: ViewMessage) => {
      posted.push(message);

      // The host answers at once, as P6 does.
      if (message.type === FRAME_READY_MESSAGE) fromHost(location("tab-a"));
    });
    frame();

    expect(posted.map((message) => message.type)).toEqual([
      FRAME_READY_MESSAGE,
      LOCATION_ACCEPTED_MESSAGE,
    ]);
  });
});

describe("a location's generation", () => {
  it("is adopted only while the frame has none", () => {
    vi.spyOn(window, "postMessage").mockImplementation(() => undefined);
    const generation = createFrameGeneration();
    frame(generation);

    fromHost(location("tab-a", "G1"));
    expect(generation.current()).toBe("G1");

    // Responses moved the frame on; the host's cached invocation trails it.
    generation.set("G2");
    fromHost(location("tab-b", "G1"));
    expect(generation.current()).toBe("G2");
  });
});

describe("experience mode", () => {
  function serve(experienceKey: string) {
    const script = document.createElement("script");
    script.id = "rhizome-view-invocation";
    script.type = "application/json";
    script.textContent = JSON.stringify({
      view: { id: "a", name: "A", origin: "bundled", experienceKey },
      context: { kind: "standalone" },
    });
    document.head.append(script);
  }

  const startHosted = async (experience: boolean) => {
    const load = vi.fn(async () => ({ default: () => <p>single page</p> }));

    await act(async () => {
      stops.push(
        await startView(
          load,
          { id: "a", name: "A", origin: "bundled" },
          {
            hosted: true,
            experience,
            reload: () => undefined,
            render: (element) => render(element),
          },
        ),
      );
    });

    return load;
  };

  it("starts a view with an experience key as a single page unless the shell asks for a frame", async () => {
    vi.spyOn(window, "postMessage").mockImplementation(() => undefined);
    serve("x");

    await startHosted(false);
    expect(screen.getByText("single page")).toBeTruthy();
  });

  it("waits for a location when the shell asks for a frame", async () => {
    const post = vi.spyOn(window, "postMessage").mockImplementation(() => undefined);
    serve("x");

    const load = await startHosted(true);
    expect(load).not.toHaveBeenCalled();
    expect(post.mock.calls.map(([message]) => message.type)).toContain(FRAME_READY_MESSAGE);
  });

  it("never announces a single view's render from an experience shell", async () => {
    document.body.innerHTML = '<div id="root"></div>';
    const host = document.createElement("iframe");
    document.body.append(host);
    const parent = host.contentWindow!;
    const sent = vi.spyOn(parent, "postMessage").mockImplementation(() => undefined);
    Object.defineProperty(window, "parent", { configurable: true, value: parent });
    serve("x");
    const types = () => sent.mock.calls.map(([message]) => message.type);

    window.history.replaceState(null, "", "/views/a?hosted=1&frame=experience");
    vi.resetModules();
    const kit = await import("./index");
    await act(() => kit.mountView(async () => ({ default: () => null }), { id: "a", name: "A" }));

    expect(types()).toContain(FRAME_READY_MESSAGE);
    expect(types()).not.toContain(VIEW_RENDERED_MESSAGE);
    expect(document.documentElement.dataset.rhizomeView).toBeUndefined();

    // The same shell without the flag is one page that announces its render.
    sent.mockClear();
    document.getElementById("root")!.id = "";
    document.body.prepend(Object.assign(document.createElement("div"), { id: "root" }));
    window.history.replaceState(null, "", "/views/a?hosted=1");
    vi.resetModules();
    const single = await import("./index");
    await act(() =>
      single.mountView(async () => ({ default: () => null }), { id: "a", name: "A" }),
    );

    expect(types()).toContain(VIEW_RENDERED_MESSAGE);
    expect(types()).not.toContain(FRAME_READY_MESSAGE);
  });
});
