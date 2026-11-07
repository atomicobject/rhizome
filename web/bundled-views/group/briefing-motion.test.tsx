import { expect, it } from "vitest";
import { commonMotionMetadata } from "./briefing-motion";

it("moves the shared type and folder to the heading while retaining exceptional metadata", () => {
  const effort = { type: "Effort note", folder: "docs/efforts" };
  expect(
    commonMotionMetadata([effort, effort, { type: "Effort workspace", folder: "docs" }]),
  ).toEqual(effort);
  expect(commonMotionMetadata([effort, { type: "Spec", folder: "docs/specs" }])).toBeUndefined();
});
