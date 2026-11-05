import { expect, it } from "vitest";
import { fitSummary } from "./overview-side-parts.tsx";

it("truncates relationship summaries at a word boundary and retains short summaries", () => {
  const measure = (text: string) => text.length;
  expect(fitSummary("Reference docs 53, Technical specs 41", 25, measure)).toBe(
    "Reference docs 53,…",
  );
  expect(fitSummary("Technical specs 1", 25, measure)).toBe("Technical specs 1");
  expect(fitSummary("Reference docs 53", 4, measure)).toBe("…");
});
