import { expect, it } from "vitest";
import { checkedAge } from "./validation-freshness.tsx";

it("formats validation freshness like Problems", () => {
  const now = Date.UTC(2026, 10, 4, 18);

  expect(checkedAge(new Date(now), now)).toBe("just now");
  expect(checkedAge(new Date(now - 5 * 60_000), now)).toBe("5m ago");
  expect(checkedAge(new Date(now - 2 * 3_600_000), now)).toBe("2h ago");
});
