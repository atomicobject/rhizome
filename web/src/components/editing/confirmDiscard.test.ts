import { afterEach, expect, it, vi } from "vitest";
import { confirmDiscard } from "./confirmDiscard";
import { writePersistedEditorDraft, readPersistedEditorDraft } from "./draftStorage";

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
  sessionStorage.clear();
});

it("confirms drafts-only discard and preserves the draft when cancelled", () => {
  writePersistedEditorDraft("vault", "note", "Unsaved work");
  const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
  expect(confirmDiscard({ opCount: 0, touchedPaths: new Set() }, "vault")).toBe(false);
  expect(confirm).toHaveBeenCalledWith("Discard local drafts? This cannot be undone.");
  expect(readPersistedEditorDraft("vault", "note")).toBe("Unsaved work");
  confirm.mockReturnValue(true);
  expect(confirmDiscard({ opCount: 0, touchedPaths: new Set() }, "vault")).toBe(true);
});
