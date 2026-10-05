import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import type { OntologyEditSessionResponse } from "../api/types";
import { INITIAL_EDIT_READ_LIFECYCLE, type EditReadLifecycle } from "../staging/stagedQuery";
import { deferredReply, emptyValidationSummariesReply, withFakeFetch } from "../test/fakeFetch";
import { renderWithQueryClient } from "../test/renderWithQueryClient";
import { baseView, execution, openEnumEditor } from "./ConfiguredView.testFixtures";
import { HomeTab } from "./HomeTab";
import { parseNotesLocation } from "./notesRoute";
import { ViewTab } from "./ViewTab";

const http = withFakeFetch();

it.each(["Home", "View"])(
  "%s binds lineage only while its reader retains pre-save rows",
  async (surface) => {
    const original = {
      notePath: "a.md",
      fragment: "item-100",
      nodeId: "a.md#item-100",
      kind: "EMBEDDED",
      structuralFingerprint: "historical",
    };

    const preview = {
      ...original,
      fragment: "item-130",
      nodeId: "a.md#item-130",
      structuralFingerprint: "saved",
    };

    const view = { ...baseView, mount: { kind: "type" as const, type: "Spec", default: true } };

    const stale = {
      ...execution([
        {
          ref: original,
          path: "a.md",
          title: "Retained",
          fields: { frontmatter: { "spec-status": "active" } },
        },
      ]),
      view,
    };

    const saved: OntologyEditSessionResponse = {
      sessionId: "saved",
      revision: 1,
      status: "clean",
      hasUncommittedChanges: false,
      createdAt: "2026-10-02T00:00:00Z",
      updatedAt: "2026-10-02T00:00:00Z",
      ops: [],
      refLineage: [{ original, preview }],
    };

    const canonical = deferredReply();
    const onStageOps = vi.fn(async () => {});
    const location = parseNotesLocation("/notes/spec", "", "");

    http.json("GET", "/api/v1/ontology/summary", {
      types: [],
      interfaces: [],
      totalNotes: 1,
      typedNotes: 1,
      issueNotes: 0,
    });
    http.json("GET", "/api/v1/views", {
      views: [view],
      targets: [
        {
          kind: "type",
          name: "Spec",
          defaultChoiceId: "table",
          choices: [{ id: "table", name: "Table", viewId: view.id, renderer: "table" }],
        },
      ],
    });
    http.json("GET", "/api/v1/ontology/types/Spec", { notes: [], count: 0 });
    http.json("GET", "/api/v1/graphs/global", { nodes: [], edges: [], truncated: false });
    http.json("GET", "/api/v2/validate", { status: "never_ran", generation: 0 });
    http.on("POST", "/api/v1/validation/summaries", emptyValidationSummariesReply);
    http.json("POST", `/api/v1/views/${view.id}/execute`, stale);

    const tab = (readLifecycle: EditReadLifecycle) =>
      surface === "Home" ? (
        <HomeTab
          summary={null}
          selection={location.selection}
          selectedType="Spec"
          location={location}
          editSession={{ session: null, readLifecycle, replaceOps: async () => {} }}
          onOpenNote={vi.fn()}
          onSelectCollection={vi.fn()}
          onStageOps={onStageOps}
        />
      ) : (
        <ViewTab
          tab={{ id: "view-test", kind: "view", viewId: view.id, title: view.name }}
          active
          editSession={{ session: null, readLifecycle }}
          onOpenNote={vi.fn()}
          onStageOps={onStageOps}
          onOpenIssues={vi.fn()}
          onTitle={vi.fn()}
        />
      );

    const { rerender } = renderWithQueryClient(tab(INITIAL_EDIT_READ_LIFECYCLE));

    await screen.findByText("Retained");
    http.on("POST", `/api/v1/views/${view.id}/execute`, () => canonical.promise);
    rerender(tab({ revision: 1, outcome: "saved", savedSession: saved }));
    fireEvent.change(openEnumEditor(), { target: { value: "archived" } });
    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({
        path: "a.md#item-130",
        nodeId: preview.nodeId,
        structuralFingerprint: preview.structuralFingerprint,
        value: "archived",
        expected: { field: { kind: "scalar", scalar: "active" } },
      }),
    ]);

    await waitFor(() => expect(http.count("POST", `/api/v1/views/${view.id}/execute`)).toBe(2));
    // A later canonical response can legitimately restore a historical strong ref.
    await act(async () =>
      canonical.resolve({ ...stale, rows: [{ ...stale.rows![0], title: "Fresh" }] }),
    );
    await screen.findByText("Fresh");
    fireEvent.change(openEnumEditor(), { target: { value: "archived" } });
    expect(onStageOps).toHaveBeenLastCalledWith([
      expect.objectContaining({
        path: "a.md#item-100",
        nodeId: original.nodeId,
        structuralFingerprint: original.structuralFingerprint,
      }),
    ]);
  },
);
