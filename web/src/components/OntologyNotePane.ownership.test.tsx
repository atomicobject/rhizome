import { fireEvent, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { NodeWorkspace } from "../api/types";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { OntologyNotePane } from "./OntologyNotePane";

it("keeps owned source read-only while global editing is enabled", async () => {
  const reason = "Managed by AO KB. Change it with the AO KB skills.";

  const workspace: NodeWorkspace = {
    requestedRef: "owned.md",
    node: {
      ref: { notePath: "owned.md", kind: "NOTE" },
      notePath: "owned.md",
      title: "Owned note",
      locator: "FILE",
      nodeLocator: {
        ref: { notePath: "owned.md", kind: "NOTE" },
        kind: "NOTE",
        sourceLocator: "owned.md",
        status: "requires_fix",
      },
    },
    content: {
      path: "owned.md",
      title: "Owned note",
      format: "markdown",
      markdown: "Owned body.",
      sourceCapabilities: ["structural_content_mutation"],
    },
    sourceRevision: { notePath: "owned.md", contentFingerprint: "base", content: "Owned body." },
    capabilities: {
      canEdit: false,
      canEditFields: false,
      canEditCollections: false,
      canNavigateChildren: true,
      canSubscribe: true,
      readOnlyReason: reason,
    },
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    loaded: { rendered: true, assessment: false, structure: false, relations: false },
    version: "owned-v1",
  };

  const stage = vi.fn();

  const { container } = renderWithQueryClient(
    <OntologyNotePane
      workspace={workspace}
      rendered={{ path: "owned.md", title: "Owned note", rendered: "Owned body." }}
      viewMode="source"
      editing
      onStageOps={stage}
      onOpen={vi.fn()}
    />,
  );

  expect(screen.getByText(reason)).toBeVisible();
  expect(screen.getByRole("button", { name: "Make linkable" })).toBeDisabled();
  fireEvent.doubleClick(await screen.findByText("Owned body."));
  expect(container.querySelector('[contenteditable="true"]')).toBeNull();
  expect(container.querySelector("textarea, input")).toBeNull();
  expect(stage).not.toHaveBeenCalled();
});
