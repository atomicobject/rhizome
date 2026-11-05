import { expect, it } from "vitest";
import { outwardSpots } from "./sigmaLabels";

it("keeps labels on the outward side with leaders no longer than 28px", () => {
  for (const [dx, dy] of [
    [1, 0],
    [-1, 0],
    [0, -1],
    [0, 1],
  ]) {
    for (const { box } of outwardSpots(100, 100, 10, 80, 14, dx, dy)) {
      const x = Math.max(box[0], Math.min(100, box[2]));
      const y = Math.max(box[1], Math.min(100, box[3]));
      expect(Math.hypot(x - 100, y - 100) - 10).toBeLessThanOrEqual(28);

      if (dx > 0) expect(box[0]).toBeGreaterThan(110);

      if (dx < 0) expect(box[2]).toBeLessThan(90);

      if (dy < 0) expect(box[3]).toBeLessThan(90);

      if (dy > 0) expect(box[1]).toBeGreaterThan(110);
    }
  }
});
