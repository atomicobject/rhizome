import { fireEvent, render, screen, within } from "@testing-library/react";
import Sigma from "sigma";
import { describe, expect, it, vi } from "vitest";
import type { GraphNode } from "../api/types";
import { installFakeWebGL } from "../test/fakeWebGL";
import { GraphView } from "./GraphView";

const baseProps = {
  nodes: [],
  edges: [],
  scope: { mode: "global" as const },
  onResetScope: () => {},
  onNodeClick: () => {},
  searchActive: false,
  searchPaths: new Set<string>(),
  searchModules: new Set<string>(),
  searchOnlyMatches: false,
  zoomKey: 0,
};

describe("GraphView", () => {
  it("shows the index-needed notice without booting the graph", () => {
    const restoreWebGL = installFakeWebGL();
    const sigmaOn = vi.spyOn(Sigma.prototype, "on");

    const view = render(
      <GraphView
        {...baseProps}
        nodes={[{ id: "note", label: "Note", kind: "note", path: "note.md" }]}
        needsIndex
      />,
    );

    try {
      expect(screen.getByText("Graph unavailable")).toBeInTheDocument();
      expect(screen.getByText("rzm index")).toBeInTheDocument();
      expect(view.container.querySelector("canvas")).toBeNull();
      expect(sigmaOn).not.toHaveBeenCalled();
    } finally {
      view.unmount();
      restoreWebGL();
      vi.restoreAllMocks();
    }
  });

  it("shows remembered display toggles and reports changes instead of keeping them", () => {
    const restoreWebGL = installFakeWebGL();
    const onDisplayChange = vi.fn();
    const display = { scaleByAuthority: true, showEdgeLabels: false, showProblems: false };

    const props = {
      ...baseProps,
      nodes: [{ id: "note", label: "Note", kind: "note", path: "note.md" }],
      display,
      onDisplayChange,
    };

    const view = render(<GraphView {...props} />);

    try {
      fireEvent.click(screen.getByRole("button", { name: "More" }));
      const labels = screen.getByRole("checkbox", { name: "Edge labels" });
      expect(labels).not.toBeChecked();

      fireEvent.click(labels);
      expect(onDisplayChange).toHaveBeenCalledWith({ showEdgeLabels: true });
      // The owner decides; until it answers, the remembered value shows.
      expect(labels).not.toBeChecked();

      view.rerender(<GraphView {...props} display={{ ...display, showEdgeLabels: true }} />);
      expect(labels).toBeChecked();
    } finally {
      view.unmount();
      restoreWebGL();
    }
  });

  it("shows an empty-state notice when no nodes are available", () => {
    render(<GraphView {...baseProps} />);

    expect(screen.getByText("No graph data yet")).toBeInTheDocument();
    expect(screen.getByText(/pick a file or folder, or rebuild the index/i)).toBeInTheDocument();
  });

  it("leaves ordinary scrolling native and zooms smoothly for pinch wheel events", () => {
    const restoreWebGL = installFakeWebGL();
    const sigmaOn = vi.spyOn(Sigma.prototype, "on");
    let view: ReturnType<typeof render> | null = null;

    try {
      view = render(
        <GraphView
          {...baseProps}
          nodes={[{ id: "note", label: "Note", kind: "note", path: "note.md" }]}
        />,
      );

      const renderer = sigmaOn.mock.contexts.find(
        (value): value is Sigma => value instanceof Sigma,
      );

      if (!renderer) throw new Error("Sigma renderer did not mount");
      const camera = renderer.getCamera();
      const canvas = renderer.getCanvases().mouse;
      const before = camera.getState();
      const scroll = new WheelEvent("wheel", { deltaY: 120, bubbles: true, cancelable: true });
      canvas.dispatchEvent(scroll);
      expect(scroll.defaultPrevented).toBe(false);
      expect(camera.getState()).toEqual(before);

      const pinch = new WheelEvent("wheel", {
        deltaY: -10,
        ctrlKey: true,
        clientX: 200,
        clientY: 150,
        bubbles: true,
        cancelable: true,
      });

      canvas.dispatchEvent(pinch);
      expect(pinch.defaultPrevented).toBe(true);
      expect(camera.getState().ratio).toBeLessThan(before.ratio);
      expect(camera.getState().ratio).toBeGreaterThan(before.ratio * 0.8);
      canvas.dispatchEvent(
        new WheelEvent("wheel", {
          deltaY: 10,
          ctrlKey: true,
          clientX: 200,
          clientY: 150,
          bubbles: true,
          cancelable: true,
        }),
      );
      expect(camera.getState().ratio).toBeCloseTo(before.ratio);
    } finally {
      view?.unmount();
      restoreWebGL();
      vi.restoreAllMocks();
    }
  });

  it.each([undefined, "_FallbackNote", "_FallbackSection"])(
    "groups internal type %s as untyped and dims nodes outside the selected legend entry",
    (fallbackType) => {
      const restoreWebGL = installFakeWebGL();
      const sigmaOn = vi.spyOn(Sigma.prototype, "on");

      const renderers = () =>
        Array.from(new Set(sigmaOn.mock.contexts)).filter(
          (value): value is Sigma => value instanceof Sigma,
        );

      const nodes: GraphNode[] = [
        { id: "spec", label: "Spec", kind: "note", path: "spec.md", resolvedType: "Spec" },
        { id: "task", label: "Task", kind: "note", path: "task.md", resolvedType: "Task" },
        {
          id: "untyped",
          label: "Untyped",
          kind: "note",
          path: "untyped.md",
          resolvedType: fallbackType,
        },
      ];

      const graphProps = {
        ...baseProps,
        nodes,
        edges: [{ source: "spec", target: "task", kind: "links_to" }],
        typeColors: new Map([
          ["Spec", "#6366f1"],
          ["Task", "#22c55e"],
          ["__untyped__", "#94a3b8"],
        ]),
        highlightTypes: new Set(["Spec"]),
        showTypeLegend: true,
      };

      let view: ReturnType<typeof render> | null = null;

      try {
        view = render(<GraphView {...graphProps} />);
        const legend = screen.getByRole("complementary", { name: "Graph type legend" });

        expect(screen.getByText("1 of 3 nodes highlighted")).toBeInTheDocument();
        expect(within(legend).getByText("Spec")).toBeInTheDocument();
        expect(within(legend).getByText("Task")).toBeInTheDocument();
        expect(within(legend).getByText("Untyped")).toBeInTheDocument();
        const specEntry = legend.querySelector('[data-type="Spec"]');
        const taskEntry = legend.querySelector('[data-type="Task"]');
        const untypedEntry = legend.querySelector('[data-type="__untyped__"]');
        expect(specEntry).toHaveClass("is-highlighted");
        expect(specEntry).toHaveTextContent("1");
        expect(taskEntry).not.toHaveClass("is-highlighted");
        expect(taskEntry).toHaveTextContent("1");
        expect(untypedEntry).toHaveTextContent("Untyped");
        expect(untypedEntry).toHaveTextContent("1");

        const renderer = renderers().at(-1);

        if (!renderer) throw new Error("Sigma renderer did not mount");
        renderer.refresh({ schedule: false });
        expect(renderer.getNodeDisplayData("task")?.hidden).toBe(false);
        expect(renderer.getNodeDisplayData("task")?.color).toBe("rgba(36, 39, 44, 0.24)");

        view.rerender(<GraphView {...graphProps} highlightTypes={new Set()} />);
        expect(screen.getByText("3 nodes")).toBeInTheDocument();
      } finally {
        view?.unmount();
        restoreWebGL();
        vi.restoreAllMocks();
      }
    },
  );

  it("enables the Problems overlay without rebuilding the graph", () => {
    const restoreWebGL = installFakeWebGL();
    const sigmaOn = vi.spyOn(Sigma.prototype, "on");
    let view: ReturnType<typeof render> | null = null;

    try {
      view = render(
        <GraphView
          {...baseProps}
          nodes={[{ id: "note", label: "Note", kind: "note", path: "note.md" }]}
          problemCounts={new Map([["note", 2]])}
        />,
      );
      const before = new Set(sigmaOn.mock.contexts).size;

      const renderer = sigmaOn.mock.contexts.find(
        (value): value is Sigma => value instanceof Sigma,
      );

      if (!renderer) throw new Error("Sigma renderer did not mount");
      const camera = renderer.getCamera();
      renderer.refresh({ schedule: false });
      expect(renderer.getNodeDisplayData("note")?.label).toBe("Note");
      fireEvent.click(screen.getByRole("button", { name: "More" }));
      fireEvent.click(screen.getByRole("checkbox", { name: "Problems (1)" }));
      renderer.refresh({ schedule: false });
      expect(renderer.getNodeDisplayData("note")?.label).toContain("2");
      fireEvent.click(screen.getByRole("checkbox", { name: "Problems (1)" }));
      renderer.refresh({ schedule: false });
      expect(renderer.getNodeDisplayData("note")?.label).toBe("Note");
      expect(new Set(sigmaOn.mock.contexts).size).toBe(before);
      expect(renderer.getCamera()).toBe(camera);
    } finally {
      view?.unmount();
      restoreWebGL();
      vi.restoreAllMocks();
    }
  });
});
