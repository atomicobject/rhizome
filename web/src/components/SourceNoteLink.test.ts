import { expect, it } from "vitest";

import { fileTitle } from "./SourceNoteLink";

it("names a source note by its file when its title has not loaded", () => {
  expect(fileTitle("docs/efforts/2026-10-07-17-15-desktop-unified-toolbar.md")).toBe(
    "Desktop unified toolbar",
  );
  expect(fileTitle("notes/pizza-party-2026.md")).toBe("Pizza party 2026");
});
