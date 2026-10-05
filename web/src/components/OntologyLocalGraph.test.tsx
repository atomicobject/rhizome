import { act, cleanup, render, screen } from "@testing-library/react";
import Sigma from "sigma";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NodeWorkspace, WorkspaceNode } from "../api/types";
import { installFakeWebGL } from "../test/fakeWebGL";
import { OntologyLocalGraph } from "./OntologyLocalGraph";

const SPEC_ID = "node|specs/demo/spec.md||";

const PLAN_ID = "node|specs/demo/plan.md||";

const VALIDATION_ID =
  "node|specs/demo/spec.md#^validation|^validation|specs/demo/spec.md#^validation";

function isSigma(value: unknown): value is Sigma {
  return value instanceof Sigma;
}

function noteNode(id: string, notePath: string, title: string): WorkspaceNode {
  return {
    id,
    kind: "note",
    ref: { notePath, kind: "NOTE" },
    notePath,
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: {
      canEdit: true,
      canEditFields: true,
      canEditCollections: true,
      canNavigateChildren: true,
      canSubscribe: true,
    },
    data: { title, locator: "FILE" },
  };
}

function embeddedNode(): WorkspaceNode {
  return {
    id: VALIDATION_ID,
    kind: "embedded",
    ref: {
      notePath: "specs/demo/spec.md",
      fragment: "^validation",
      nodeId: "specs/demo/spec.md#^validation",
      kind: "EMBEDDED",
    },
    notePath: "specs/demo/spec.md",
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: {
      canEdit: true,
      canEditFields: true,
      canEditCollections: true,
      canNavigateChildren: true,
      canSubscribe: true,
    },
    data: {
      title: "Validation story",
      locator: "EMBEDDED",
      fragment: "^validation",
      resolvedType: "Story",
    },
  };
}

function makeWorkspace(overrides: Partial<NodeWorkspace> = {}): NodeWorkspace {
  return {
    requestedRef: "specs/demo/spec.md",
    focusedNodeId: SPEC_ID,
    node: {
      ref: { notePath: "specs/demo/spec.md", kind: "NOTE" },
      notePath: "specs/demo/spec.md",
      title: "Demo spec",
      locator: "FILE",
    },
    content: {
      path: "specs/demo/spec.md",
      title: "Demo spec",
    },
    nodes: [],
    edges: [],
    views: {
      localGraph: {
        centerNodeIds: [],
        nodeIds: [],
      },
    },
    loaded: {
      rendered: true,
      assessment: true,
      structure: true,
      relations: true,
    },
    capabilities: {
      canEdit: true,
      canEditFields: true,
      canEditCollections: true,
      canNavigateChildren: true,
      canSubscribe: true,
    },
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    version: "v1",
    ...overrides,
  };
}

function linkedWorkspace(): NodeWorkspace {
  return makeWorkspace({
    views: {
      localGraph: {
        centerNodeIds: [SPEC_ID],
        nodeIds: [SPEC_ID, PLAN_ID],
      },
    },
    nodes: [
      noteNode(SPEC_ID, "specs/demo/spec.md", "Demo spec"),
      noteNode(PLAN_ID, "specs/demo/plan.md", "Demo plan"),
    ],
    edges: [{ kind: "links_to", fromId: SPEC_ID, toId: PLAN_ID }],
  });
}

/**
 * Click a node on the live renderer the component mounted. `on` runs once per
 * renderer during setup, so its recorded contexts are the Sigma instances.
 */
function renderers(onSpy: { mock: { contexts: unknown[] } }): Sigma[] {
  return Array.from(new Set(onSpy.mock.contexts)).filter(isSigma);
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

describe("OntologyLocalGraph", () => {
  let restoreWebGL: () => void = () => undefined;

  beforeEach(() => {
    restoreWebGL = installFakeWebGL();
  });

  afterEach(() => {
    cleanup();
    restoreWebGL();
  });

  it("shows the empty-state copy when the local graph has no nodes", () => {
    const { container } = render(
      <OntologyLocalGraph
        workspace={makeWorkspace({
          views: {
            localGraph: {
              centerNodeIds: [],
              nodeIds: [],
              emptyReason: "No local graph yet.",
            },
          },
        })}
        onOpen={vi.fn()}
      />,
    );

    expect(screen.getByText("Local graph")).toBeInTheDocument();
    expect(screen.getByText("No local graph yet")).toBeInTheDocument();
    expect(screen.getByText("No local graph yet.")).toBeInTheDocument();
    expect(container.querySelector(".graph-container .graph-zoom")).not.toBeNull();
    expect(container.querySelector(".ontology-local-graph__header .graph-zoom")).toBeNull();
  });

  it("keeps the renderer alive across unchanged rerenders", () => {
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const killSpy = vi.spyOn(Sigma.prototype, "kill");
    const onOpen = vi.fn();
    const workspace = linkedWorkspace();

    const { rerender } = render(<OntologyLocalGraph workspace={workspace} onOpen={onOpen} />);
    expect(renderers(onSpy)).toHaveLength(1);

    rerender(<OntologyLocalGraph workspace={workspace} onOpen={onOpen} />);

    expect(renderers(onSpy)).toHaveLength(1);
    expect(killSpy).not.toHaveBeenCalled();

    onSpy.mockRestore();
    killSpy.mockRestore();
  });

  it("routes note-node clicks to stack or beside opens", () => {
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const onOpen = vi.fn();
    render(<OntologyLocalGraph workspace={linkedWorkspace()} onOpen={onOpen} />);

    clickNode(renderers(onSpy)[0], PLAN_ID);

    expect(onOpen).toHaveBeenCalledWith("specs/demo/plan.md", "stack");
    clickNode(renderers(onSpy)[0], PLAN_ID, new MouseEvent("click", { metaKey: true }));

    expect(onOpen).toHaveBeenLastCalledWith("specs/demo/plan.md", "beside");
    onSpy.mockRestore();
  });

  it("drills embedded nodes in place and opens beside on a modifier click", () => {
    const onSpy = vi.spyOn(Sigma.prototype, "on");
    const onOpen = vi.fn();
    const onOpenNode = vi.fn();
    render(
      <OntologyLocalGraph
        workspace={makeWorkspace({
          focusedNodeId: SPEC_ID,
          views: {
            localGraph: {
              centerNodeIds: [VALIDATION_ID],
              nodeIds: [SPEC_ID, VALIDATION_ID],
            },
          },
          nodes: [noteNode(SPEC_ID, "specs/demo/spec.md", "Demo spec"), embeddedNode()],
          edges: [{ kind: "embeds", fromId: SPEC_ID, toId: VALIDATION_ID }],
        })}
        onOpen={onOpen}
        onOpenNode={onOpenNode}
      />,
    );

    clickNode(renderers(onSpy)[0], VALIDATION_ID);

    expect(onOpenNode).toHaveBeenCalledWith(
      expect.objectContaining({
        nodeId: "specs/demo/spec.md#^validation",
        notePath: "specs/demo/spec.md",
        locator: "EMBEDDED",
      }),
    );
    onOpenNode.mockClear();

    clickNode(renderers(onSpy)[0], VALIDATION_ID, new MouseEvent("click", { ctrlKey: true }));

    expect(onOpenNode).not.toHaveBeenCalled();
    expect(onOpen).toHaveBeenCalledWith("specs/demo/spec.md#^validation", "beside");
    onSpy.mockRestore();
  });
});
