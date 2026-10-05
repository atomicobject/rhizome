import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import Sigma from "sigma";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { GraphResponse, OntologySummaryResponse, OntologyTypeResponse } from "../api/types";
import { installFakeWebGL } from "../test/fakeWebGL";
import { buildHomeProblemCounts, HomeGraphLayout } from "./HomeGraphLayout";

const summary: OntologySummaryResponse = {
  schemaPresent: true,
  totalNotes: 2,
  typedNotes: 2,
  untypedNotes: 0,
  ambiguousNotes: 0,
  issueNotes: 0,
  types: [{ name: "ConcreteType", label: "Concrete type", count: 2, issueCount: 0, role: "note" }],
  interfaces: [
    {
      name: "Traceable",
      label: "Traceable",
      count: 2,
      issueCount: 0,
      implementors: ["ConcreteType"],
    },
  ],
};

const typeDetail: OntologyTypeResponse = {
  count: 2,
  issueCount: 0,
  type: {
    name: "Traceable",
    label: "Traceable",
    pluralLabel: "Traceable notes",
    fields: [],
  },
  notes: [
    {
      ref: { notePath: "notes/one.md", kind: "NOTE" },
      path: "notes/one.md",
      title: "One",
      resolvedType: "ConcreteType",
      relationCount: 1,
      updatedAt: 1_713_000_000,
      hasIssues: false,
    },
    {
      ref: { notePath: "notes/two.md", kind: "NOTE" },
      path: "notes/two.md",
      title: "Two",
      resolvedType: "ConcreteType",
      relationCount: 1,
      updatedAt: 1_713_000_100,
      hasIssues: false,
    },
  ],
};

const graph: GraphResponse = {
  nodes: [
    {
      id: "one",
      label: "One",
      kind: "note",
      path: "notes/one.md",
      resolvedType: "ConcreteType",
    },
    {
      id: "two",
      label: "Two",
      kind: "note",
      path: "notes/two.md",
      resolvedType: "ConcreteType",
    },
  ],
  edges: [{ source: "one", target: "two", kind: "link" }],
  truncated: false,
};

const typeColors = new Map<string, string>([["ConcreteType", "#6366f1"]]);

it("keeps missing graph validation summaries unknown", () => {
  const counts = buildHomeProblemCounts(
    graph,
    new Map([
      [
        "note:notes/one.md",
        {
          scope: { kind: "note", key: "notes/one.md" } as const,
          issueCount: 0,
          affectedFileCount: 0,
          affectedNoteCount: 0,
          repairActionCount: 0,
        },
      ],
    ]),
  );

  expect(counts.get("one")).toBe(0);
  expect(counts.has("two")).toBe(false);
});

function layoutProps(
  mode: "all" | "type",
  onOpenNote: (path: string, mode?: "activate" | "beside") => void,
) {
  if (mode === "all") {
    return {
      mode,
      summary,
      selectedType: null,
      typeDetail,
      graph,
      typeColors,
      onOpenNote,
      onRetry: () => {},
      onRetryGraph: () => {},
      onOpenIssues: () => {},
    } as const;
  }

  return {
    mode,
    summary,
    selectedType: "Traceable",
    typeDetail,
    graph,
    typeColors,
    onOpenNote,
    onRetry: () => {},
    onRetryGraph: () => {},
  } as const;
}

describe("HomeGraphLayout", () => {
  let restoreWebGL: () => void;

  beforeEach(() => {
    restoreWebGL = installFakeWebGL();
  });

  afterEach(() => {
    cleanup();
    restoreWebGL();
  });

  it("distinguishes graph loading, failure, and empty success while preserving type content", async () => {
    const onRetryGraph = vi.fn();
    const props = layoutProps("type", vi.fn());

    const { rerender } = render(
      <HomeGraphLayout {...props} graph={null} graphLoading onRetryGraph={onRetryGraph} />,
    );

    expect(screen.getByText("Loading graph…")).toBeVisible();
    expect(screen.queryByText("No graph data yet")).toBeNull();
    expect(screen.getAllByText("One").length).toBeGreaterThan(0);

    rerender(
      <HomeGraphLayout
        {...props}
        graph={null}
        graphError="Graph service failed"
        onRetryGraph={onRetryGraph}
      />,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Graph service failed");
    expect(screen.queryByText("No graph data yet")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Retry graph" }));
    expect(onRetryGraph).toHaveBeenCalledOnce();

    rerender(
      <HomeGraphLayout
        {...props}
        graph={{ nodes: [], edges: [], truncated: false }}
        onRetryGraph={onRetryGraph}
      />,
    );
    expect(await screen.findByText("No graph data yet")).toBeVisible();
  });

  it("keeps the loaded graph mounted during refresh and refresh failure", async () => {
    const props = layoutProps("all", vi.fn());
    const { container, rerender } = render(<HomeGraphLayout {...props} />);
    await waitFor(() => expect(container.querySelector(".graph-surface canvas")).not.toBeNull());
    const canvas = container.querySelector(".graph-surface canvas");
    rerender(<HomeGraphLayout {...props} graphLoading />);
    expect(screen.getByText("Refreshing graph…")).toBeVisible();
    expect(container.querySelector(".graph-surface canvas")).toBe(canvas);
    rerender(<HomeGraphLayout {...props} graphError="Refresh failed" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Refresh failed");
    expect(container.querySelector(".graph-surface canvas")).toBe(canvas);

    rerender(
      <HomeGraphLayout
        {...props}
        validationSummaries={
          new Map([
            [
              "note:notes/one.md",
              {
                scope: { kind: "note", key: "notes/one.md" },
                issueCount: 1,
                affectedFileCount: 1,
                affectedNoteCount: 1,
                repairActionCount: 0,
              },
            ],
          ])
        }
      />,
    );
    expect(container.querySelector(".graph-surface canvas")).toBe(canvas);
  });

  it("keeps one global graph renderer while selection changes its type highlights", async () => {
    const onOpenNote = vi.fn();
    const sigmaOn = vi.spyOn(Sigma.prototype, "on");
    const { container, rerender } = render(<HomeGraphLayout {...layoutProps("all", onOpenNote)} />);

    const initialSurface = await waitFor(() => {
      const surface = container.querySelector(".graph-surface");

      if (!surface) throw new Error("the global graph renderer did not mount");

      return surface;
    });

    const canvas = initialSurface.querySelector("canvas");
    const renderer = sigmaOn.mock.contexts.find((value): value is Sigma => value instanceof Sigma);

    if (!renderer || !canvas) throw new Error("the Sigma renderer did not mount");
    const camera = renderer.getCamera();
    const cameraState = camera.getState();

    rerender(<HomeGraphLayout {...layoutProps("type", onOpenNote)} />);

    await waitFor(() => expect(container.querySelector(".graph-surface")).toBe(initialSurface));
    expect(initialSurface.querySelector("canvas")).toBe(canvas);
    expect(new Set(sigmaOn.mock.contexts)).toContain(renderer);
    expect(new Set(sigmaOn.mock.contexts).size).toBe(1);
    expect(renderer.getCamera()).toBe(camera);
    expect(camera.getState()).toEqual(cameraState);
    await waitFor(() =>
      expect(container.querySelector('[data-type="ConcreteType"]')).toHaveClass("is-highlighted"),
    );

    const event = {
      x: 0,
      y: 0,
      sigmaDefaultPrevented: false,
      preventSigmaDefault: () => undefined,
      original: new MouseEvent("click", { metaKey: true }),
    };

    renderer.emit("clickNode", { node: "one", event, preventSigmaDefault: () => undefined });
    expect(onOpenNote).toHaveBeenCalledWith("notes/one.md", "beside");
  });
});
