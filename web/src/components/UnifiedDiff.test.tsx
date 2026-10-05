import { render } from "@testing-library/react";
import { expect, it } from "vitest";
import { UnifiedDiff } from "./UnifiedDiff";

function renderedRows(diff: string) {
  const { container } = render(<UnifiedDiff diff={diff} />);
  const outer = container.querySelector(".unified-diff");
  expect(outer).toHaveAttribute("tabindex", "0");

  return Array.from(container.querySelectorAll(".unified-diff__line")).map((row) => {
    const [oldNumber, newNumber] = Array.from(row.querySelectorAll(".unified-diff__num"));
    expect(oldNumber).toHaveAttribute("aria-hidden", "true");
    expect(newNumber).toHaveAttribute("aria-hidden", "true");

    return {
      kind: row.className.replace("unified-diff__line", "").trim(),
      oldNumber: oldNumber.textContent,
      newNumber: newNumber.textContent,
      text: row.textContent?.slice(
        (oldNumber.textContent || "").length + (newNumber.textContent || "").length,
      ),
    };
  });
}

it("numbers rendered lines of a unified diff from each hunk header", () => {
  const diff = [
    "--- a/note.md",
    "+++ b/note.md",
    "@@ -4,3 +4,3 @@",
    " stage: pursuing",
    "-- [ ] 100% [[Atomic Con|AtomicCon]]",
    "+- [x] 100% [[Atomic Con|AtomicCon]]",
    "--- rule",
    "@@ -20 +20,2 @@",
    " last",
    "+tail",
    "\\ No newline at end of file",
    "",
  ].join("\n");

  expect(renderedRows(diff)).toEqual([
    { kind: "is-hunk", oldNumber: "", newNumber: "", text: "⋯" },
    { kind: "", oldNumber: "4", newNumber: "4", text: " stage: pursuing" },
    {
      kind: "is-remove",
      oldNumber: "5",
      newNumber: "",
      text: "-- [ ] 100% [[Atomic Con|AtomicCon]]",
    },
    { kind: "is-add", oldNumber: "", newNumber: "5", text: "+- [x] 100% [[Atomic Con|AtomicCon]]" },
    { kind: "is-remove", oldNumber: "6", newNumber: "", text: "--- rule" },
    { kind: "is-hunk", oldNumber: "", newNumber: "", text: "⋯" },
    { kind: "", oldNumber: "20", newNumber: "20", text: " last" },
    { kind: "is-add", oldNumber: "", newNumber: "21", text: "+tail" },
    { kind: "is-hunk", oldNumber: "", newNumber: "", text: "\\ No newline at end of file" },
  ]);
});

it("numbers rendered creation hunks from new line one", () => {
  expect(renderedRows("--- a/n.md\n+++ b/n.md\n@@ -0,0 +1,2 @@\n+a\n+b\n")).toEqual([
    { kind: "is-hunk", oldNumber: "", newNumber: "", text: "⋯" },
    { kind: "is-add", oldNumber: "", newNumber: "1", text: "+a" },
    { kind: "is-add", oldNumber: "", newNumber: "2", text: "+b" },
  ]);
});
