import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { editorDraftStorageKey, readPersistedEditorDraftRecord } from "../editing/draftStorage";
import { DateWidget } from "./DateWidget";

afterEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
});

describe("DateWidget", () => {
  it("renders the literal value in browse mode", () => {
    render(<DateWidget value="2026-04-12" editing={false} />);
    expect(screen.getByText("2026-04-12")).toBeInTheDocument();
  });

  it("renders an empty placeholder when missing", () => {
    render(<DateWidget value="" editing={false} />);
    expect(screen.getByText("(empty)")).toBeInTheDocument();
  });

  it("renders a date input pre-populated with ISO form in edit mode", () => {
    render(<DateWidget value="2026-04-12T14:00:00Z" editing={true} />);
    const input = screen.getByDisplayValue<HTMLInputElement>("2026-04-12");
    expect(input.type).toBe("date");
  });

  it("stages DateTime edits as RFC3339 while preserving the existing offset", () => {
    const onStage = vi.fn();
    render(
      <DateWidget
        value="2026-04-12T14:30-04:00"
        kind="datetime"
        editing={true}
        onStage={onStage}
        ariaLabel="updatedAt"
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("updatedAt");
    expect(input.type).toBe("datetime-local");
    expect(input).toHaveValue("2026-04-12T14:30");
    expect(screen.getByText("-04:00")).toBeVisible();

    fireEvent.change(input, { target: { value: "2026-04-13T09:15" } });
    fireEvent.blur(input);

    expect(onStage).toHaveBeenCalledWith("2026-04-13T09:15:00-04:00");
  });

  it("clears authored seconds and fractional precision after a minute-aligned DateTime change", () => {
    const onStage = vi.fn();
    render(
      <DateWidget
        value="2026-04-12T14:30:17.123456-04:00"
        kind="datetime"
        editing={true}
        onStage={onStage}
        ariaLabel="updatedAt"
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("updatedAt");
    expect(input).toHaveValue("2026-04-12T14:30:17.123");
    expect(input).toHaveAttribute("step", "0.000001");

    fireEvent.change(input, { target: { value: "2026-04-13T09:15" } });
    fireEvent.blur(input);

    expect(onStage).toHaveBeenCalledWith("2026-04-13T09:15:00-04:00");
  });

  it("preserves authored fractional precision when date edits normalize zero seconds", () => {
    const onStage = vi.fn();
    render(
      <DateWidget
        value="2026-04-12T14:30:00.000100-04:00"
        kind="datetime"
        editing={true}
        onStage={onStage}
        ariaLabel="updatedAt"
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("updatedAt");

    fireEvent.change(input, { target: { value: "2026-04-13T09:15" } });
    fireEvent.blur(input);

    expect(onStage).toHaveBeenCalledWith("2026-04-13T09:15:00.000100-04:00");
  });

  it("stores a field draft under its vault owner and clears it after staging", () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "date-test");
    const operationTarget = "field:test:due";
    const key = editorDraftStorageKey("/vault", operationTarget);
    expect(key).not.toBeNull();
    const onStage = vi.fn();
    render(
      <DateWidget
        value="2026-04-12"
        editing
        vaultKey="/vault"
        operationTarget={operationTarget}
        onStage={onStage}
        ariaLabel="due"
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("due");
    fireEvent.change(input, { target: { value: "2026-05-01" } });
    expect(readPersistedEditorDraftRecord("/vault", operationTarget)?.value).toBe("2026-05-01");
    expect(input.type).toBe("date");
    fireEvent.blur(input);

    expect(onStage).toHaveBeenCalledWith("2026-05-01");
    expect(window.localStorage.getItem(key!)).toBeNull();
  });

  it("does not stage when blurred without a change", () => {
    const onStage = vi.fn();
    render(<DateWidget value="2026-04-12" editing={true} onStage={onStage} />);
    fireEvent.blur(screen.getByDisplayValue("2026-04-12"));
    expect(onStage).not.toHaveBeenCalled();
  });

  it("keeps an empty required DateTime draft local and reports the error", () => {
    const onStage = vi.fn();
    render(
      <DateWidget
        value=""
        kind="datetime"
        required
        editing={true}
        onStage={onStage}
        ariaLabel="updatedAt"
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("updatedAt");
    fireEvent.blur(input);

    expect(input).toHaveValue("");
    expect(onStage).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("updatedAt is required.");
  });

  it("keeps an invalid authored value visible while offering a repair input", () => {
    render(<DateWidget value="someday" editing={true} ariaLabel="due" />);
    expect(screen.getByLabelText("due")).toHaveValue("");
    expect(screen.getByText("Current value: someday")).toBeVisible();
  });

  it("cancels with Escape without accepting on blur", () => {
    const onStage = vi.fn();
    render(<DateWidget value="2026-04-12" editing={true} onStage={onStage} />);
    const input = screen.getByDisplayValue("2026-04-12");
    fireEvent.change(input, { target: { value: "2026-05-01" } });
    input.focus();
    expect(input).toHaveFocus();
    fireEvent.keyDown(input, { key: "Escape" });
    expect(input).toHaveValue("2026-04-12");
    fireEvent.blur(input);
    expect(onStage).not.toHaveBeenCalled();
  });
});
