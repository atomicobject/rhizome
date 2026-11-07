import { expect, it } from "vitest";
import { relativeAge } from "./ConfiguredTableCellReadOnly";

it.each([
  [59 / 60, "59m"],
  [1, "1h"],
  [23, "23h"],
  [23.99, "23h"],
  [24, "1d"],
  [30, "1d"],
])("formats %s hours as %s", (hours, expected) => {
  const now = Date.UTC(2026, 10, 4);
  expect(relativeAge(now - Number(hours) * 3_600_000, now)).toBe(expected);
});
