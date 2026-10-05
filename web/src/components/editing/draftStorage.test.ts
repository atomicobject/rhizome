import { beforeEach, describe, expect, it } from "vitest";

import {
  clearPersistedEditorDraft,
  clearPersistedEditorDrafts,
  editorDraftStorageKey,
  hasPersistedEditorDrafts,
  readPersistedEditorDraft,
  readPersistedEditorDraftRecord,
  writePersistedEditorDraft,
} from "./draftStorage";

const TAB_ID_KEY = "rhizome:ontology-edit-session:tab-id";

describe("draftStorage", () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    window.sessionStorage.setItem(TAB_ID_KEY, "tab-a");
  });

  it("persists raw text, including an empty draft, under the vault, tab, and target", () => {
    const key = editorDraftStorageKey("/vault/a", "field:notes/a.md:title");

    writePersistedEditorDraft("/vault/a", "field:notes/a.md:title", "");

    if (!key) throw new Error("expected a draft storage key");
    expect(key).toBe("rhizome:edit-draft:%2Fvault%2Fa:tab-a:field%3Anotes%2Fa.md%3Atitle");
    expect(window.localStorage.getItem(key)).toBe(JSON.stringify({ version: 1, value: "" }));
    expect(readPersistedEditorDraft("/vault/a", "field:notes/a.md:title")).toBe("");
    expect(hasPersistedEditorDrafts("/vault/a")).toBe(true);
  });

  it("preserves the first expected witness while a draft is rewritten", () => {
    const expected = {
      field: { kind: "scalar" as const, scalar: "before" },
      sourceHash: "source-before",
      sourceContent: "before",
    };

    writePersistedEditorDraft("/vault/a", "field:notes/a.md:title", "draft", expected);
    writePersistedEditorDraft("/vault/a", "field:notes/a.md:title", "newer-draft", {
      field: { kind: "scalar", scalar: "after" },
      sourceHash: "source-after",
      sourceContent: "after",
    });

    expect(readPersistedEditorDraftRecord("/vault/a", "field:notes/a.md:title")).toEqual({
      version: 1,
      value: "newer-draft",
      expected,
    });
  });

  it("recovers a legacy raw draft without inventing a witness", () => {
    const key = editorDraftStorageKey("/vault/a", "field:notes/a.md:title");

    if (!key) throw new Error("expected a draft storage key");
    window.localStorage.setItem(key, "legacy draft");

    expect(readPersistedEditorDraftRecord("/vault/a", "field:notes/a.md:title")).toEqual({
      version: 1,
      value: "legacy draft",
      legacy: true,
    });
  });

  it("clears only the current vault and browser tab", () => {
    writePersistedEditorDraft("/vault/a", "narrative:notes/a.md", "current vault");
    writePersistedEditorDraft("/vault/b", "narrative:notes/a.md", "other vault");
    window.sessionStorage.setItem(TAB_ID_KEY, "tab-b");
    writePersistedEditorDraft("/vault/a", "narrative:notes/a.md", "other tab");
    window.sessionStorage.setItem(TAB_ID_KEY, "tab-a");

    clearPersistedEditorDrafts("/vault/a");

    expect(readPersistedEditorDraft("/vault/a", "narrative:notes/a.md")).toBeNull();
    expect(hasPersistedEditorDrafts("/vault/a")).toBe(false);
    window.sessionStorage.setItem(TAB_ID_KEY, "tab-b");
    expect(readPersistedEditorDraft("/vault/a", "narrative:notes/a.md")).toBe("other tab");
    window.sessionStorage.setItem(TAB_ID_KEY, "tab-a");
    expect(readPersistedEditorDraft("/vault/b", "narrative:notes/a.md")).toBe("other vault");
  });

  it("clears one canonical target without touching another target", () => {
    writePersistedEditorDraft("/vault/a", "source:notes/a.md", "source");
    writePersistedEditorDraft("/vault/a", "narrative:notes/a.md", "narrative");

    clearPersistedEditorDraft("/vault/a", "source:notes/a.md");

    expect(readPersistedEditorDraft("/vault/a", "source:notes/a.md")).toBeNull();
    expect(readPersistedEditorDraft("/vault/a", "narrative:notes/a.md")).toBe("narrative");
  });
});
