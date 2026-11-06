import { FolderObstacles } from "./folderObstacles";
import { expect, it } from "vitest";
import { folderLinkLength } from "./sigmaFolderLinks";

it("hides child routes until hover or selection", () => {
  expect(folderLinkLength(true, false, 600)).toBe(0);
  expect(folderLinkLength(true, true, 600)).toBe(600);
  expect(folderLinkLength(false, false, 600)).toBe(600);
  expect(folderLinkLength(true, false, 20)).toBe(0);
});

it("checks only nearby obstacles for a route in a 10,000-node layout", () => {
  const points = Array.from({ length: 10000 }, (_, i) => ({
    id: String(i),
    x: (i % 100) / 100,
    y: Math.floor(i / 100) / 100,
    size: 1,
  }));

  const index = new FolderObstacles(points);
  const candidates = [...index.nearRoute({ x: 0.2, y: 0.2 }, { x: 0.22, y: 0.22 }, 0.005)];
  expect(candidates.length).toBeLessThan(200);
  expect(candidates.some((p) => p.id === "2121")).toBe(true);
  const diagonal = [...index.nearRoute({ x: 0, y: 0 }, { x: 1, y: 1 }, 0.005)];
  expect(diagonal.length).toBeLessThan(2500);
});
