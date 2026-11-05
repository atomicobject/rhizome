import { expect, it } from "vitest";
import { folderLinkLength } from "./sigmaFolderLinks";

it("hides child routes until hover or selection", () => {
  expect(folderLinkLength(true, false, 600)).toBe(0);
  expect(folderLinkLength(true, true, 600)).toBe(600);
  expect(folderLinkLength(false, false, 600)).toBe(600);
  expect(folderLinkLength(true, false, 20)).toBe(0);
});
