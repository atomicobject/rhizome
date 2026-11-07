import { expect, it } from "vitest";
import fixture from "./fixtures/folder-overview.json";
import { folderOverview } from "./folderOverview";

it("matches the server's file-pair projection using the original browser rules", () => {
  expect(folderOverview(fixture.graph)).toEqual(fixture.overview);
});
