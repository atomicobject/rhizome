import { act, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { ViewCatalogEntry, ViewChoice } from "../api/types";
import {
  EDIT_SESSION_MESSAGE,
  FRAME_READY_MESSAGE,
  LOCATION_ACCEPTED_MESSAGE,
  LOCATION_MESSAGE,
  LOCATION_RENDERED_MESSAGE,
  RESUME_MESSAGE,
  SUSPEND_MESSAGE,
  type ViewMessage,
} from "../lib/customViewMessages";
import { ViewHost } from "../views/ViewHost";
import { ExperienceFrameLayer, ExperienceTabContext } from "./ExperienceFrameLayer";

const definition = (id: string): ViewCatalogEntry => ({
  id,
  name: id,
  experienceKey: "exp_kb",
  source: { kind: "custom", entry: `${id}.tsx` },
  mount: { kind: "standalone" },
  defaults: {},
  variants: {},
  definition: {
    apiVersion: "rhizome.view.v1",
    id,
    name: id,
    source: { kind: "custom", entry: `${id}.tsx` },
    mount: { kind: "standalone" },
    variants: {},
  },
});

const choice = (id: string): ViewChoice => ({
  id,
  name: id,
  viewId: id,
  renderer: "custom",
  experienceKey: "exp_kb",
});

const services = { session: null, onOpenNote: vi.fn(), onStageOps: vi.fn(async () => undefined) };

function Tab({ id, view, active }: { id: string; view: string; active: boolean }) {
  return (
    <ExperienceTabContext value={id}>
      <ViewHost
        view={{ id: view, name: view }}
        choice={choice(view)}
        definition={definition(view)}
        context={{ kind: "standalone" }}
        active={active}
        services={services}
        embedded
      />
    </ExperienceTabContext>
  );
}

function Shell({ children, open }: { children: ReactNode; open: string[] }) {
  return (
    <ExperienceFrameLayer session={null} openTabIds={new Set(open)} vaultKey="v">
      {children}
    </ExperienceFrameLayer>
  );
}

const frames = () => document.querySelectorAll<HTMLIFrameElement>("iframe.experience-frame");

afterEach(() => vi.restoreAllMocks());

const fromFrame = (frame: HTMLIFrameElement, data: ViewMessage) =>
  act(async () =>
    window.dispatchEvent(
      new MessageEvent("message", {
        origin: window.location.origin,
        source: frame.contentWindow,
        data,
      }),
    ),
  );

const frameReady = (frame: HTMLIFrameElement) => fromFrame(frame, { type: FRAME_READY_MESSAGE });

describe("ExperienceFrameLayer", () => {
  it("shows an experience's tabs of any kind in one frame laid over the active placeholder", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(
          JSON.stringify({ view: { id: "v", name: "v" }, context: { kind: "standalone" } }),
          {
            headers: { "Content-Type": "application/json", "Rhizome-Generation": "g1" },
          },
        ),
    );
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        return this.classList.contains("experience-placeholder")
          ? DOMRect.fromRect({ x: 40, y: 90, width: 600, height: 400 })
          : DOMRect.fromRect({ x: 10, y: 20, width: 900, height: 700 });
      },
    );

    const { rerender } = render(
      <Shell open={["home", "note:a.md"]}>
        <Tab id="home" view="kb.home" active />
        <Tab id="note:a.md" view="kb.assertion" active={false} />
      </Shell>,
    );

    expect(screen.getByRole("status")).toHaveTextContent("Loading view…");
    await waitFor(() => expect(frames()).toHaveLength(1));
    const [frame] = frames();

    expect(frame?.getAttribute("src")).toBe("/views/kb.home?hosted=1&frame=experience");
    // The layer is in the content's document order, after the tab's placeholder.
    expect(
      screen.getByRole("status").compareDocumentPosition(frame!) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    await waitFor(() =>
      expect(frame?.style).toMatchObject({
        top: "70px",
        left: "30px",
        width: "600px",
        height: "400px",
      }),
    );

    const posted = vi.spyOn(frame!.contentWindow!, "postMessage");

    rerender(
      <Shell open={["home", "note:a.md"]}>
        <Tab id="home" view="kb.home" active={false} />
        <Tab id="note:a.md" view="kb.assertion" active />
      </Shell>,
    );

    await act(async () => undefined);
    expect(frames()).toHaveLength(1);
    expect(frames()[0]).toBe(frame);
    // Nothing goes to a frame that has not said it listens.
    expect(posted.mock.calls.filter(([message]) => message.type === LOCATION_MESSAGE)).toEqual([]);
  });

  // A resize lays out the page the frame shows, so it waits until the frame swapped pages.
  it("keeps a frame's box until it accepts the new tab's location", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(
          JSON.stringify({ view: { id: "v", name: "v" }, context: { kind: "standalone" } }),
          { headers: { "Content-Type": "application/json", "Rhizome-Generation": "g1" } },
        ),
    );
    let placeholder = { width: 600, height: 400 };
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        return this.classList.contains("experience-placeholder")
          ? DOMRect.fromRect(placeholder)
          : DOMRect.fromRect({ width: 900, height: 700 });
      },
    );

    const tabs = (active: string) => (
      <Shell open={["home", "note:a.md"]}>
        <Tab id="home" view="kb.home" active={active === "home"} />
        <Tab id="note:a.md" view="kb.assertion" active={active === "note:a.md"} />
      </Shell>
    );

    const { rerender } = render(tabs("home"));
    await waitFor(() => expect(frames()).toHaveLength(1));
    const [frame] = frames();
    const located: { tabId: string; navigationId: string }[] = [];
    vi.spyOn(frame!.contentWindow!, "postMessage").mockImplementation(
      (message: { type: string; tabId: string; navigationId: string }) => {
        if (message.type === LOCATION_MESSAGE) located.push(message);
      },
    );
    await frameReady(frame!);
    await waitFor(() => expect(located).toHaveLength(1));
    await fromFrame(frame!, { ...located[0], type: LOCATION_ACCEPTED_MESSAGE });
    await fromFrame(frame!, { ...located[0], type: LOCATION_RENDERED_MESSAGE, at: 1 });
    await waitFor(() => expect(frame?.style.width).toBe("600px"));

    placeholder = { width: 300, height: 200 };
    rerender(tabs("note:a.md"));
    await waitFor(() => expect(located.at(-1)?.tabId).toBe("note:a.md"));
    await act(
      async () => new Promise((resolve) => requestAnimationFrame(() => resolve(undefined))),
    );
    expect(frame?.style.width).toBe("600px");

    await fromFrame(frame!, { ...located.at(-1)!, type: LOCATION_ACCEPTED_MESSAGE });
    await waitFor(() => expect(frame?.style).toMatchObject({ width: "300px", height: "200px" }));
  });

  it("suspends the frame when its tab shows another view and resumes it on return", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () =>
        new Response(
          JSON.stringify({ view: { id: "v", name: "v" }, context: { kind: "standalone" } }),
          {
            headers: { "Content-Type": "application/json" },
          },
        ),
    );

    const App = ({ experience }: { experience: boolean }) => (
      <Shell open={["home"]}>
        <ExperienceTabContext value="home">
          <ViewHost
            view={{ id: "x", name: "x" }}
            choice={
              experience ? choice("kb.home") : { ...choice("other"), experienceKey: undefined }
            }
            definition={
              experience
                ? definition("kb.home")
                : { ...definition("other"), experienceKey: undefined }
            }
            context={{ kind: "standalone" }}
            active
            services={services}
            embedded
          />
        </ExperienceTabContext>
      </Shell>
    );

    const { rerender } = render(<App experience />);
    await waitFor(() => expect(frames()).toHaveLength(1));
    const [frame] = frames();
    const sent: string[] = [];
    vi.spyOn(frame!.contentWindow!, "postMessage").mockImplementation((message: { type: string }) =>
      sent.push(message.type),
    );
    await frameReady(frame!);
    expect(sent).toEqual([LOCATION_MESSAGE, EDIT_SESSION_MESSAGE]);

    rerender(<App experience={false} />);
    await act(async () => undefined);
    // Hidden and inert at once; suspended after the grace period.
    expect(frame).toHaveAttribute("inert");
    await waitFor(() => expect(sent.at(-1)).toBe(SUSPEND_MESSAGE));

    rerender(<App experience />);
    await act(async () => undefined);
    expect(frames()[0]).toBe(frame);
    // The same page at the same session needs only the resume.
    expect(sent.slice(3)).toEqual([RESUME_MESSAGE]);
  });
});
