import { act, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { ViewCatalogEntry, ViewChoice } from "../api/types";
import { LOCATION_MESSAGE } from "../lib/customViewMessages";
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
});
