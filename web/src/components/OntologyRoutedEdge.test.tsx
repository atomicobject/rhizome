import { render } from "@testing-library/react";
import { type EdgeProps, Position } from "@xyflow/react";
import { describe, expect, it } from "vitest";

import { OntologyRoutedEdge } from "./OntologyRoutedEdge";

const edgeProps: EdgeProps = {
  id: "edge-1",
  source: "Spec",
  target: "Plan",
  sourceX: 180,
  sourceY: 92,
  targetX: 440,
  targetY: 118,
  sourcePosition: Position.Right,
  targetPosition: Position.Left,
  data: {
    routedPoints: [
      { x: 240, y: 40 },
      { x: 320, y: 40 },
      { x: 320, y: 140 },
      { x: 400, y: 140 },
    ],
  },
};

describe("OntologyRoutedEdge", () => {
  it("keeps the endpoint segments orthogonal when live handles move", () => {
    const { container } = render(
      <svg>
        <OntologyRoutedEdge {...edgeProps} />
      </svg>,
    );

    const path = container.querySelector("path.react-flow__edge-path");
    expect(path?.getAttribute("d")).toBe("M 180 92 L 320 92 L 320 118 L 440 118");
  });

  it("adds an orthogonal dogleg when a straight route no longer aligns", () => {
    const { container } = render(
      <svg>
        <OntologyRoutedEdge
          {...edgeProps}
          data={{
            routedPoints: [
              { x: 180, y: 92 },
              { x: 440, y: 92 },
            ],
          }}
        />
      </svg>,
    );

    expect(container.querySelector("path.react-flow__edge-path")?.getAttribute("d")).toBe(
      "M 180 92 L 310 92 L 310 118 L 440 118",
    );
  });
});
