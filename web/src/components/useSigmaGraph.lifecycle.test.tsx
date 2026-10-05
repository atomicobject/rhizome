import { act, render } from "@testing-library/react";
import type { ReactNode } from "react";
import Sigma from "sigma";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GraphEdge, GraphNode } from "../api/types";
import { installFakeWebGL } from "../test/fakeWebGL";
import { useSigmaGraph } from "./useSigmaGraph";

const firstNode: GraphNode = {
  id: "one",
  label: "One",
  kind: "note",
  path: "one.md",
};

const secondNode: GraphNode = {
  id: "two",
  label: "Two",
  kind: "note",
  path: "two.md",
};

const emptySet = new Set<string>();

const emptyEdges: GraphEdge[] = [];

const globalScope = { mode: "global" } as const;

function Harness({
  nodes,
  onNodeClick,
  highlightTypes,
}: {
  nodes: GraphNode[];
  onNodeClick: (node: GraphNode) => void;
  highlightTypes?: ReadonlySet<string>;
}): ReactNode {
  const { containerRef, hover } = useSigmaGraph({
    nodes,
    edges: emptyEdges,
    scope: globalScope,
    onNodeClick,
    searchActive: false,
    searchPaths: emptySet,
    searchModules: emptySet,
    searchOnlyMatches: false,
    highlightTypes,
    zoomKey: 0,
  });

  return (
    <>
      <div ref={containerRef} />
      {hover && <div data-testid="graph-hover">{hover.label}</div>}
    </>
  );
}

function isSigma(value: unknown): value is Sigma {
  return value instanceof Sigma;
}

function clickNode(sigma: Sigma, node: string, original = new MouseEvent("click")) {
  const event = {
    x: 0,
    y: 0,
    sigmaDefaultPrevented: false,
    preventSigmaDefault: () => undefined,
    original,
  };

  act(() => {
    sigma.emit("clickNode", { node, event, preventSigmaDefault: () => undefined });
  });
}

describe("useSigmaGraph renderer lifecycle", () => {
  let restoreWebGL: () => void = () => undefined;

  beforeEach(() => {
    restoreWebGL = installFakeWebGL();
  });

  afterEach(() => {
    restoreWebGL();
    vi.restoreAllMocks();
  });

  it("keeps the renderer for callback changes and dispatches through the latest callback", () => {
    // WHY: `on` runs once per renderer during setup, so its recorded contexts
    // are the live Sigma instances, in creation order. Spying on the real
    // prototype keeps the renderer real.
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const killSpy = vi.spyOn(Sigma.prototype, "kill");
    const renderers = () => Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);

    const firstCallback = vi.fn();
    const latestCallback = vi.fn();
    const nodes = [firstNode];
    const view = render(<Harness nodes={nodes} onNodeClick={firstCallback} />);

    expect(renderers()).toHaveLength(1);
    view.rerender(<Harness nodes={nodes} onNodeClick={latestCallback} />);
    expect(renderers()).toHaveLength(1);

    clickNode(renderers()[0], "one");
    expect(firstCallback).not.toHaveBeenCalled();
    expect(latestCallback).toHaveBeenCalledWith(
      expect.objectContaining({ id: "one", path: "one.md" }),
      { beside: false },
    );

    view.rerender(<Harness nodes={[firstNode, secondNode]} onNodeClick={latestCallback} />);
    expect(renderers()).toHaveLength(2);
    expect(killSpy.mock.contexts).toEqual([renderers()[0]]);

    view.unmount();
    expect(killSpy.mock.contexts).toEqual([renderers()[0], renderers()[1]]);
  });

  it("refreshes highlight reducers without rebuilding the renderer or camera", () => {
    // WHY: the mount layout refresh animates the camera with Date.now() and
    // requestAnimationFrame. Under real timers a loaded worker can still be
    // mid-animation when the test places its camera state, and the final frame
    // overwrites it. Faking those clocks makes the animation finish on demand.
    vi.useFakeTimers({
      toFake: [
        "setTimeout",
        "clearTimeout",
        "requestAnimationFrame",
        "cancelAnimationFrame",
        "Date",
      ],
    });
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const killSpy = vi.spyOn(Sigma.prototype, "kill");
    const refreshSpy = vi.spyOn(Sigma.prototype, "refresh");
    const renderers = () => Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);
    const nodes = [{ ...firstNode, resolvedType: "Other", nodeId: "node:one" }];
    const callback = vi.fn();

    const view = render(
      <Harness nodes={nodes} onNodeClick={callback} highlightTypes={new Set<string>()} />,
    );

    try {
      const renderer = renderers().at(-1);

      if (!renderer) throw new Error("Sigma renderer did not mount");
      const camera = renderer.getCamera();
      const animate = vi.spyOn(camera, "animate");

      // Run the 120ms mount layout refresh, then its 350ms camera animation to
      // completion, before placing the state the highlight refresh must keep.
      act(() => vi.advanceTimersByTime(120));
      expect(animate).toHaveBeenCalledWith(
        { x: 0.5, y: 0.5, angle: 0, ratio: 1 },
        { duration: 350 },
      );
      act(() => vi.advanceTimersByTime(400));
      expect(camera.isAnimated()).toBe(false);

      camera.setState({ x: 0.23, y: 0.71, angle: 0.2, ratio: 1.7 });
      const cameraBefore = camera.getState();
      const refreshesBefore = refreshSpy.mock.calls.length;

      view.rerender(
        <Harness nodes={nodes} onNodeClick={callback} highlightTypes={new Set(["Spec"])} />,
      );
      act(() => vi.advanceTimersToNextFrame());

      expect(renderers()).toHaveLength(1);
      expect(killSpy).not.toHaveBeenCalled();
      expect(camera.getState()).toEqual(cameraBefore);
      expect(refreshSpy.mock.calls.length).toBeGreaterThan(refreshesBefore);
    } finally {
      view.unmount();
      vi.useRealTimers();
    }
  });

  it("refreshes the current renderer after graph replacement during mount layout delay", () => {
    vi.useFakeTimers();
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const killSpy = vi.spyOn(Sigma.prototype, "kill");
    const renderers = () => Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);
    const callback = vi.fn();
    const view = render(<Harness nodes={[firstNode]} onNodeClick={callback} />);

    try {
      const stale = renderers()[0];
      const staleResize = vi.spyOn(stale, "resize");
      const staleRefresh = vi.spyOn(stale, "refresh");
      view.rerender(<Harness nodes={[secondNode]} onNodeClick={callback} />);
      const current = renderers()[1];
      expect(current).toBeDefined();
      expect(killSpy.mock.contexts).toContain(stale);
      const currentResize = vi.spyOn(current, "resize");
      const currentRefresh = vi.spyOn(current, "refresh");
      const camera = current.getCamera();
      // The camera starts at ratio 1, so move it first; a hard-coded ratio must fail.
      camera.setState({ ratio: 0.5 });
      const animate = vi.spyOn(camera, "animate");
      const staleResizeBefore = staleResize.mock.calls.length;
      const staleRefreshBefore = staleRefresh.mock.calls.length;
      const currentResizeBefore = currentResize.mock.calls.length;
      const currentRefreshBefore = currentRefresh.mock.calls.length;

      act(() => vi.advanceTimersByTime(120));

      expect(staleResize).toHaveBeenCalledTimes(staleResizeBefore);
      expect(staleRefresh).toHaveBeenCalledTimes(staleRefreshBefore);
      expect(currentResize.mock.calls.length).toBeGreaterThan(currentResizeBefore);
      expect(currentRefresh.mock.calls.length).toBeGreaterThan(currentRefreshBefore);
      expect(animate).toHaveBeenCalledWith(
        { x: 0.5, y: 0.5, angle: 0, ratio: 0.5 },
        { duration: 350 },
      );
    } finally {
      view.unmount();
      vi.useRealTimers();
    }
  });

  it("keeps a faded node hoverable and clickable, including beside modifier", () => {
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const renderers = () => Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);

    const node = {
      ...firstNode,
      resolvedType: "Other",
      path: "docs/spec.md#^story-a",
      notePath: "docs/spec.md",
      nodeId: "story-a",
      sourceLocator: "docs/spec.md#^story-a",
      nodeRef: {
        notePath: "docs/spec.md",
        kind: "EMBEDDED" as const,
        nodeId: "story-a",
        fragment: "^story-a",
      },
    };

    const callback = vi.fn();

    const view = render(
      <Harness nodes={[node]} onNodeClick={callback} highlightTypes={new Set(["Spec"])} />,
    );

    const renderer = renderers().at(-1);

    if (!renderer) throw new Error("Sigma renderer did not mount");

    act(() => {
      const event = {
        x: 0,
        y: 0,
        sigmaDefaultPrevented: false,
        preventSigmaDefault: () => undefined,
        original: new MouseEvent("mousemove"),
      };

      renderer.emit("enterNode", { node: "one", event, preventSigmaDefault: () => undefined });
    });
    expect(view.getByTestId("graph-hover")).toHaveTextContent("One");

    clickNode(renderer, "one", new MouseEvent("click", { metaKey: true }));
    expect(callback).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "one",
        path: "docs/spec.md#^story-a",
        notePath: "docs/spec.md",
        nodeId: "story-a",
        sourceLocator: "docs/spec.md#^story-a",
        nodeRef: node.nodeRef,
        resolvedType: "Other",
      }),
      { beside: true },
    );
    view.unmount();
  });
});
