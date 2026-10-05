import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import type {
  ModifiedNotesResponse,
  OntologyEditOp,
  OntologyEditSessionResponse,
} from "../api/types";
import { jsonReply, withFakeFetch } from "../test/fakeFetch";
import { NotesModifiedHome } from "./NotesModifiedHome";

const http = withFakeFetch();

const DIFF_PATH = "/api/v1/edit-sessions/session-1/diff";

const ops: OntologyEditOp[] = [
  { id: "op-title", kind: "setField", path: "notes/alpha.md", field: "title", value: "Alpha" },
  { id: "op-tags", kind: "setLinkField", path: "notes/alpha.md", field: "tags", values: ["x"] },
  { id: "op-body", kind: "setNarrative", path: "notes/beta.md", markdown: "new body" },
];

function session(sessionOps: OntologyEditOp[]): OntologyEditSessionResponse {
  return {
    sessionId: "session-1",
    revision: 4,
    status: "dirty",
    ops: sessionOps,
    hasUncommittedChanges: sessionOps.length > 0,
    createdAt: "2026-09-14T00:00:00Z",
    updatedAt: "2026-09-14T00:00:00Z",
  };
}

const twoNotes: ModifiedNotesResponse = {
  sessionId: "session-1",
  totals: { notes: 2, ops: 3, setField: 1, setLinkField: 1, setNarrative: 1 },
  notes: [
    {
      path: "notes/alpha.md",
      title: "Alpha",
      resolvedType: "Note",
      hasMaterialChange: true,
      ops: [
        {
          id: "op-title",
          kind: "setField",
          nodeRef: { notePath: "notes/alpha.md", kind: "note" },
          field: "title",
          previousValue: "Old",
          value: "Alpha",
        },
        {
          id: "op-tags",
          kind: "setLinkField",
          nodeRef: { notePath: "notes/alpha.md", kind: "note" },
          field: "tags",
          previousValues: [],
          values: ["x"],
        },
      ],
    },
    {
      path: "notes/beta.md",
      title: "Beta",
      hasMaterialChange: true,
      ops: [
        {
          id: "op-body",
          kind: "setNarrative",
          nodeRef: { notePath: "notes/beta.md", kind: "note" },
          markdown: "new body",
        },
      ],
    },
  ],
  updatedAt: "2026-09-14T00:00:00Z",
};

const emptyDiff: ModifiedNotesResponse = {
  sessionId: "session-1",
  totals: { notes: 0, ops: 0 },
  notes: [],
  updatedAt: "2026-09-14T00:00:00Z",
};

it("summarizes staged notes and changes in the status line", async () => {
  http.on("POST", DIFF_PATH, () => jsonReply(twoNotes));
  render(<NotesModifiedHome session={session(ops)} onOpenNote={vi.fn()} onReplaceOps={vi.fn()} />);

  await screen.findByText("2 notes · 3 changes · 1 narrative · 2 fields");
  expect(screen.getByRole("heading", { name: "Changes" })).toBeTruthy();
});

it("discarding one op replaces the session ops without that op id", async () => {
  http.on("POST", DIFF_PATH, () => jsonReply(twoNotes));
  const onReplaceOps = vi.fn();
  render(
    <NotesModifiedHome session={session(ops)} onOpenNote={vi.fn()} onReplaceOps={onReplaceOps} />,
  );

  const discards = await screen.findAllByRole("button", { name: "Discard this change" });
  fireEvent.click(discards[1]);

  expect(onReplaceOps).toHaveBeenCalledTimes(1);
  expect(onReplaceOps.mock.calls[0][0].map((op: OntologyEditOp) => op.id)).toEqual([
    "op-title",
    "op-body",
  ]);
});

it("names the item an in-note change targets", async () => {
  const itemOp: OntologyEditOp = {
    id: "op-item",
    kind: "setField",
    path: "notes/alpha.md#item-12856",
    field: "done",
    value: "true",
  };

  http.on("POST", DIFF_PATH, () =>
    jsonReply({
      ...twoNotes,
      totals: { notes: 1, ops: 1, setField: 1 },
      notes: [
        {
          path: "notes/alpha.md",
          title: "Alpha",
          hasMaterialChange: true,
          ops: [
            {
              id: "op-item",
              kind: "setField",
              nodeRef: { notePath: "notes/alpha.md", fragment: "item-12856", kind: "embedded" },
              nodeTitle: "Send the renewal quote",
              field: "done",
              previousValue: "false",
              value: "true",
            },
          ],
        },
      ],
    }),
  );
  render(
    <NotesModifiedHome session={session([itemOp])} onOpenNote={vi.fn()} onReplaceOps={vi.fn()} />,
  );

  expect(await screen.findByText("Send the renewal quote")).toBeTruthy();
});

it("reports an empty session as nothing staged", async () => {
  http.on("POST", DIFF_PATH, () => jsonReply(emptyDiff));
  render(<NotesModifiedHome session={session([])} onOpenNote={vi.fn()} onReplaceOps={vi.fn()} />);

  await screen.findByText("Nothing staged");
  expect(screen.queryByRole("status")).toBeNull();
});
