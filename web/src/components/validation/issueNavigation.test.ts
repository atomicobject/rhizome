import { describe, expect, it } from "vitest";
import {
  diagnosticNoteTarget,
  issueTargetToTextRange,
  parseIssueTarget,
  utf8ByteRangeToLineRange,
} from "./issueNavigation";

describe("issue navigation", () => {
  it("targets the canonical node when validation provides one", () => {
    expect(
      diagnosticNoteTarget({
        issueKey: "issue-1",
        check: "ontology",
        primaryPath: "notes/café.md",
        location: { unit: "utf8_bytes", start: 0, end: 2, nodeId: "REQ-1" },
      }),
    ).toBe("notes/café.md#issue:node=REQ-1&unit=utf8_bytes&start=0&end=2");
  });

  it("round trips field and relation locators without losing punctuation", () => {
    const fragment = diagnosticNoteTarget({
      issueKey: "issue-2",
      check: "ontology",
      primaryPath: "notes/a.md",
      location: {
        unit: "line",
        start: 7,
        end: 7,
        nodeId: "SPEC-1",
        field: "acceptance criteria",
        relation: "implements/spec",
      },
    })?.split("#")[1];

    expect(parseIssueTarget(fragment)).toMatchObject({
      nodeId: "SPEC-1",
      field: "acceptance criteria",
      relation: "implements/spec",
      start: 7,
    });
  });

  it("converts UTF-8 byte offsets without treating CRLF as two lines", () => {
    const source = "é\r\nsecond\nthird";
    expect(utf8ByteRangeToLineRange(source, 4, 10)).toEqual({ startLine: 2, endLine: 2 });
  });

  it("converts UTF-8 byte offsets to editor character offsets", () => {
    expect(
      issueTargetToTextRange("é\r\nsecond", { unit: "utf8_bytes", start: 4, end: 10 }),
    ).toEqual({ from: 2, to: 8 });
  });
});
