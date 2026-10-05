import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  editorDraftStorageKey,
  hasPersistedEditorDrafts,
  readPersistedEditorDraftRecord,
} from "../editing/draftStorage";
import { InlineTextWidget } from "./InlineTextWidget";

describe("InlineTextWidget", () => {
  it("renders the value as static text in browse mode", () => {
    render(<InlineTextWidget value="SPEC-0007" editing={false} />);
    expect(screen.getByText("SPEC-0007")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("renders a placeholder when the value is missing", () => {
    render(<InlineTextWidget value="" editing={false} present={false} placeholder="(empty)" />);
    expect(screen.getByText("(empty)")).toBeInTheDocument();
  });

  it("stages on blur when the value changes in edit mode", () => {
    const onStage = vi.fn();
    render(<InlineTextWidget value="v1.0" editing={true} onStage={onStage} />);
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "v1.1" } });
    fireEvent.blur(input);
    expect(onStage).toHaveBeenCalledWith("v1.1");
  });

  it("does not stage when blurred without a change", () => {
    const onStage = vi.fn();
    render(<InlineTextWidget value="v1.0" editing={true} onStage={onStage} />);
    fireEvent.blur(screen.getByRole("textbox"));
    expect(onStage).not.toHaveBeenCalled();
  });

  it("clears a persisted draft when typing back to the authored value", () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "inline-test");
    const vaultKey = "/vault";
    const operationTarget = "field:test:title";
    const storageKey = editorDraftStorageKey(vaultKey, operationTarget);
    const onStage = vi.fn();
    render(
      <InlineTextWidget
        value="alpha"
        editing={true}
        onStage={onStage}
        vaultKey={vaultKey}
        operationTarget={operationTarget}
      />,
    );
    const input = screen.getByRole<HTMLInputElement>("textbox");

    fireEvent.change(input, { target: { value: "bravo" } });
    expect(readPersistedEditorDraftRecord(vaultKey, operationTarget)?.value).toBe("bravo");
    fireEvent.change(input, { target: { value: "alpha" } });
    fireEvent.blur(input);

    expect(window.localStorage.getItem(storageKey!)).toBeNull();
    expect(hasPersistedEditorDrafts(vaultKey)).toBe(false);
    expect(onStage).not.toHaveBeenCalled();
  });

  it("commits on Enter and reverts on Escape", () => {
    const onStage = vi.fn();
    render(<InlineTextWidget value="alpha" editing={true} onStage={onStage} />);
    const input = screen.getByRole<HTMLInputElement>("textbox");

    fireEvent.change(input, { target: { value: "bravo" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onStage).toHaveBeenCalledWith("bravo");
    expect(onStage).toHaveBeenCalledTimes(1);
    onStage.mockClear();

    fireEvent.change(input, { target: { value: "charlie" } });
    fireEvent.keyDown(input, { key: "Escape" });
    // Escape resets the draft to the on-disk value; blur after revert should
    // not trigger another stage.
    fireEvent.blur(input);
    expect(onStage).not.toHaveBeenCalled();
  });

  it("does not commit while an IME composition is active", () => {
    const onStage = vi.fn();
    render(<InlineTextWidget value="alpha" editing={true} onStage={onStage} />);
    const input = screen.getByRole<HTMLInputElement>("textbox");

    fireEvent.compositionStart(input);
    fireEvent.change(input, { target: { value: "あ" } });
    fireEvent.keyDown(input, { key: "Enter", keyCode: 229 });
    fireEvent.blur(input);
    expect(onStage).not.toHaveBeenCalled();

    fireEvent.compositionEnd(input);
    fireEvent.blur(input);
    expect(onStage).toHaveBeenCalledTimes(1);
    expect(onStage).toHaveBeenCalledWith("あ");
  });

  it("stages a dirty draft when navigation unmounts the input", () => {
    const onStage = vi.fn();
    const { unmount } = render(<InlineTextWidget value="alpha" editing={true} onStage={onStage} />);
    const input = screen.getByRole<HTMLInputElement>("textbox");

    fireEvent.change(input, { target: { value: "unfinished" } });
    unmount();

    expect(onStage).toHaveBeenCalledTimes(1);
    expect(onStage).toHaveBeenCalledWith("unfinished");
  });

  it("does not clear a newer draft when an earlier staged value is acknowledged", () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "inline-test");
    const vaultKey = "/vault";
    const operationTarget = "field:test:title";
    const onStage = vi.fn();

    const { rerender } = render(
      <InlineTextWidget
        value="alpha"
        editing
        onStage={onStage}
        vaultKey={vaultKey}
        operationTarget={operationTarget}
      />,
    );

    const input = screen.getByRole<HTMLInputElement>("textbox");
    fireEvent.change(input, { target: { value: "bravo" } });
    fireEvent.blur(input);
    fireEvent.change(input, { target: { value: "charlie" } });

    rerender(
      <InlineTextWidget
        value="bravo"
        editing
        onStage={onStage}
        vaultKey={vaultKey}
        operationTarget={operationTarget}
      />,
    );

    expect(screen.getByRole<HTMLInputElement>("textbox")).toHaveValue("charlie");
    expect(readPersistedEditorDraftRecord(vaultKey, operationTarget)?.value).toBe("charlie");
  });

  it("stages a superseding value when text returns to the disk value", () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "inline-test");
    const vaultKey = "/vault";
    const operationTarget = "field:test:title";
    const onStage = vi.fn();
    render(
      <InlineTextWidget
        value="alpha"
        editing
        onStage={onStage}
        vaultKey={vaultKey}
        operationTarget={operationTarget}
        onDraft={() => ({
          field: { kind: "scalar", scalar: "alpha" },
          sourceHash: "hash-alpha",
        })}
      />,
    );
    const input = screen.getByRole<HTMLInputElement>("textbox");
    fireEvent.change(input, { target: { value: "bravo" } });
    fireEvent.blur(input);
    fireEvent.change(input, { target: { value: "alpha" } });
    expect(readPersistedEditorDraftRecord(vaultKey, operationTarget)).toMatchObject({
      value: "alpha",
      expected: { sourceHash: "hash-alpha" },
    });
    fireEvent.blur(input);

    expect(onStage).toHaveBeenNthCalledWith(1, "bravo");
    expect(onStage).toHaveBeenNthCalledWith(2, "alpha");
  });
});
