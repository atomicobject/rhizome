import { describe, expect, it } from "vitest";

import type { ViewTableRow } from "../api/types";
import {
  type EditCandidate,
  candidateForInput,
  recordDisplayTitle,
} from "./ConfiguredTableCellHelpers";

function candidate(path: string, label: string): EditCandidate {
  return { ref: { notePath: path, kind: "NOTE" }, value: `[[${path}|${label}]]`, label, path };
}

describe("candidateForInput", () => {
  it("matches a bare wikilink by file name when the title differs", () => {
    const doc = candidate("Notes/Process - Company Document.md", "Process for Atomic Object");

    expect(candidateForInput([doc], "[[Process - Company Document]]")).toBe(doc);
  });

  it("prefers an exact match over an earlier file-name match", () => {
    const byFile = candidate("archive/task.md", "Old task");
    const exact = candidate("work/current.md", "task");

    expect(candidateForInput([byFile, exact], "[[task]]")).toBe(exact);
  });

  it("does not guess between notes that share a file name", () => {
    const first = candidate("a/task.md", "First");
    const second = candidate("b/task.md", "Second");

    expect(candidateForInput([first, second], "[[task]]")).toBeUndefined();
  });
});

describe("recordDisplayTitle", () => {
  it("uses the full embedded display label while leaving the summary untouched", () => {
    const summary = "The capped summary";
    const title = "The full title " + "with more words ".repeat(10) + "end";

    const row = {
      ref: { notePath: "note.md", kind: "EMBEDDED" },
      title,
      fields: { summary },
    } satisfies ViewTableRow;

    expect(recordDisplayTitle(row, summary)).toBe(title);
    expect(row.fields.summary).toBe(summary);
  });
});
