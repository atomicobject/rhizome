import { describe, expect, it } from "vitest";
import { formatDue, formatEvery, formatRelative, formatRunAge } from "./taskFormat";

const now = Date.parse("2026-11-04T18:00:00Z");

const day = 86_400_000;

describe("task schedule wording", () => {
  it("uses words and singular units for intervals and future due times", () => {
    expect(formatEvery("7d")).toBe("7 days");
    expect(formatEvery("1d")).toBe("1 day");
    expect(formatEvery("12h")).toBe("12 hours");
    expect(formatEvery("1h")).toBe("1 hour");
    expect(formatRelative(new Date(now + 6 * day).toISOString(), now)).toBe("in 6 days");
    expect(formatDue(new Date(now + 6 * day).toISOString(), false, now)).toBe("In 6 days");
    expect(formatDue(new Date(now + day).toISOString(), false, now)).toBe("In 24 hours");
    expect(formatDue(new Date(now - day).toISOString(), false, now)).toBe("Due now");
  });
});

it("names run ages without reading as a duration", () => {
  expect(formatRunAge(new Date(now - 27 * 3_600_000).toISOString(), now)).toBe("1d ago");
  expect(formatRunAge(new Date(now - 13 * 3_600_000).toISOString(), now)).toBe("13h ago");
  expect(formatRunAge(new Date(now - 3_600_000).toISOString(), now)).toBe("1h ago");
  expect(formatRunAge(new Date(now - 10_000).toISOString(), now)).toBe("just now");
  expect(formatRunAge(new Date(now + day).toISOString(), now)).toBe("just now");
});
