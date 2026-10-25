import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { KeyboardShortcuts } from "./KeyboardShortcuts";

describe("KeyboardShortcuts", () => {
  const proto = HTMLDialogElement.prototype;

  const original = {
    showModal: Object.getOwnPropertyDescriptor(proto, "showModal"),
    close: Object.getOwnPropertyDescriptor(proto, "close"),
  };

  beforeEach(() => {
    // jsdom has no modal dialog support; this proves our handlers, not native focus trapping.
    Object.defineProperty(proto, "showModal", {
      configurable: true,
      value(this: HTMLDialogElement) {
        this.open = true;
      },
    });
    Object.defineProperty(proto, "close", {
      configurable: true,
      value(this: HTMLDialogElement) {
        this.open = false;
      },
    });
  });

  afterEach(() => {
    for (const [name, descriptor] of Object.entries(original)) {
      if (descriptor) Object.defineProperty(proto, name, descriptor);
      else Reflect.deleteProperty(proto, name);
    }
  });

  it("opens on ? outside text fields and from its header button", () => {
    render(
      <>
        <input aria-label="Search" />
        <KeyboardShortcuts />
      </>,
    );

    const dialog = () => screen.queryByRole("dialog", { name: "Keyboard shortcuts" });

    fireEvent.keyDown(screen.getByRole("textbox", { name: "Search" }), { key: "?" });
    expect(dialog()).toBeNull();

    fireEvent.keyDown(document.body, { key: "?" });
    expect(dialog()).toBeVisible();
    expect(screen.getByText("Edit cell")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(dialog()).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Keyboard shortcuts" }));
    expect(dialog()).toBeVisible();
  });
  it("ignores ? inside rich editors and custom text boxes", () => {
    render(
      <>
        <div contentEditable suppressContentEditableWarning>
          <p data-testid="paragraph">Draft</p>
        </div>
        <div role="textbox" tabIndex={0} aria-label="Code" />
        <KeyboardShortcuts />
      </>,
    );

    fireEvent.keyDown(screen.getByTestId("paragraph"), { key: "?" });
    fireEvent.keyDown(screen.getByRole("textbox", { name: "Code" }), { key: "?" });

    expect(screen.queryByRole("dialog", { name: "Keyboard shortcuts" })).toBeNull();
  });

  it("lets the reader turn the ? shortcut off and remembers the choice", () => {
    const first = render(<KeyboardShortcuts />);

    fireEvent.click(screen.getByRole("button", { name: "Keyboard shortcuts" }));
    const toggle = screen.getByRole("checkbox", { name: "Open this list with the ? key" });
    expect(toggle).toBeChecked();
    fireEvent.click(toggle);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    fireEvent.keyDown(document.body, { key: "?" });
    expect(screen.queryByRole("dialog", { name: "Keyboard shortcuts" })).toBeNull();
    expect(screen.getByRole("button", { name: "Keyboard shortcuts" })).not.toHaveAttribute(
      "aria-keyshortcuts",
    );

    first.unmount();
    render(<KeyboardShortcuts />);
    fireEvent.keyDown(document.body, { key: "?" });
    expect(screen.queryByRole("dialog", { name: "Keyboard shortcuts" })).toBeNull();
    window.localStorage.clear();
  });

  it("opens from the desktop toolbar when the page has no button", () => {
    render(<KeyboardShortcuts trigger={false} />);

    expect(screen.queryByRole("button", { name: "Keyboard shortcuts" })).toBeNull();
    act(() => {
      window.dispatchEvent(
        new CustomEvent("rhizome:desktop", { detail: { command: "shortcuts" } }),
      );
    });

    expect(screen.getByRole("dialog", { name: "Keyboard shortcuts" })).toHaveAttribute("open");
    screen.getByRole<HTMLDialogElement>("dialog", { name: "Keyboard shortcuts" }).close();

    fireEvent.keyDown(document.body, { key: "?" });
    expect(screen.getByRole("dialog", { name: "Keyboard shortcuts" })).toHaveAttribute("open");
  });
});
