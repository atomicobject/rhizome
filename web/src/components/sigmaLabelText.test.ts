import { expect, it } from "vitest";
import { noteLabelLines } from "./sigmaLabelText";

it("wraps note titles to two 220px lines before shortening with one ellipsis", () => {
  const measure = (s: string) => s.length * 7;
  expect(noteLabelLines("Short... title", measure)).toEqual(["Short… title"]);

  const lines = noteLabelLines(
    "Search strategy and indexing: distinguishing implementation words with an exceptionally long title ending",
    measure,
  );

  expect(lines).toHaveLength(2);
  expect(lines.every((s) => measure(s) <= 220)).toBe(true);
  expect(lines[1]).toContain("…");
  expect(lines[1]).toMatch(/ending$/);
});

it("balances a title that only slightly exceeds one line", () => {
  const lines = noteLabelLines("unified-search-answer-architecture", (s) => s.length * 7);
  expect(lines).toEqual(["unified-search-", "answer-architecture"]);
});
