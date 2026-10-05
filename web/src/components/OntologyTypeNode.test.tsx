import { render, screen } from "@testing-library/react";
import { type NodeProps, ReactFlowProvider } from "@xyflow/react";
import { describe, expect, it } from "vitest";

import type { OntologyNodeData } from "../lib/ontologyGraph";
import { OntologyTypeNode } from "./OntologyTypeNode";

const baseData: OntologyNodeData = {
  name: "Spec",
  count: 3,
  issueCount: 0,
  isInterface: false,
  role: "note",
  implements: [],
  schemaTargets: [],
  fields: [],
  scalarFieldCount: 0,
  relationCount: 0,
  sectionCount: 0,
};

function nodeProps(data: OntologyNodeData): NodeProps & { data: OntologyNodeData } {
  return {
    id: "Spec",
    type: "ontologyType",
    data,
    selected: false,
    dragging: false,
    zIndex: 0,
    draggable: false,
    selectable: true,
    deletable: false,
    isConnectable: false,
    positionAbsoluteX: 0,
    positionAbsoluteY: 0,
  };
}

function renderNode(data: OntologyNodeData) {
  return render(
    <ReactFlowProvider>
      <OntologyTypeNode {...nodeProps(data)} />
    </ReactFlowProvider>,
  );
}

describe("OntologyTypeNode", () => {
  it("makes the entire ontology node link to the ontology type page", () => {
    renderNode({ ...baseData, description: "A governing artifact." });

    const link = screen.getByRole("link", { name: /Spec/ });
    expect(link).toHaveAttribute("href", "/ontology/type/Spec");
    expect(link).toHaveTextContent("Spec");
    expect(link).toHaveTextContent("3");
    expect(link).toHaveTextContent("A governing artifact.");
  });

  it("renders compact survey details from the atlas cards", () => {
    renderNode({
      ...baseData,
      issueCount: 2,
      description: "A governing artifact.",
      implements: ["SpecLike"],
      scalarFieldCount: 5,
      relationCount: 2,
      sectionCount: 1,
    });

    const link = screen.getByRole("link", { name: /Spec/ });
    expect(link).toHaveTextContent("5 fields");
    expect(link).toHaveTextContent("2 rel");
    expect(link).toHaveTextContent("1 sec");
    expect(link).toHaveTextContent("2 issues");
    expect(link).toHaveTextContent("SpecLike");
  });
});
