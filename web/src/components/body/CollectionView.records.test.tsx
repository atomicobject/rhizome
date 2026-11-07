import { screen, within } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { NodeRef, WorkspaceContentNode } from "../../api/types";
import { renderWithQueryClient } from "../../test/renderWithQueryClient";
import { CollectionView } from "./CollectionView";
import { workspaceWithEmbeddedRelation } from "./noteInfoFixtures.test-support";

it("keeps all 201 child refs counted when only 199 bodies are loaded and avoids duplicate titles", () => {
  const workspace = workspaceWithEmbeddedRelation();

  const source = {
    representation: "source_value" as const,
    text: '"Check availability"',
    mediaType: "application/json",
    value: { kind: "string" as const, scalar: "Check availability" },
  };

  workspace.content.source = source;

  const refs: NodeRef[] = Array.from({ length: 201 }, (_, index) => ({
    notePath: "services.yaml",
    kind: "EMBEDDED",
    nodeId: `job-${index}`,
    fragment: `record:${index}`,
  }));

  const nodes: WorkspaceContentNode[] = refs.slice(0, 199).map((ref, index) => ({
    id: ref.nodeId || "",
    kind: "embedded",
    ref,
    notePath: ref.notePath,
    status: workspace.status,
    capabilities: workspace.capabilities,
    data: {
      source,
      title: index ? `Job ${index}` : "Check availability",
      binding: { properties: { id: index ? `JOB-${index}` : "Check availability" } },
    },
  }));

  workspace.nodes = nodes;
  const open = vi.fn();

  const { container } = renderWithQueryClient(
    <CollectionView
      block={{ fieldName: "jobs", childRefs: refs }}
      context={{
        workspace,
        mode: "view",
        onOpenNode: open,
        lookupNode: (id) => nodes.find((node) => node.id === id),
      }}
      node={nodes[0]}
      kindOrdinal={0}
    />,
  );

  expect(container.querySelector(".body-collection__count")).toHaveTextContent("201");
  expect(screen.getByRole("status")).toHaveTextContent("2 more not loaded");
  const first = screen.getByRole("button", { name: "Check availability" });
  expect(within(first).getAllByText("Check availability")).toHaveLength(1);
  expect(screen.getByRole("button", { name: "JOB-1Job 1" })).toBeVisible();
  expect(container.querySelectorAll(".body-collection__row")).toHaveLength(199);
});
