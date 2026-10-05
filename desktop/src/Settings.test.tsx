import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { Settings } from "./Settings";

it("replaces the editable path when Browse selects another executable", () => {
  const onSelect = vi.fn();
  const props = {
    info: null,
    busy: false,
    onRefresh: vi.fn(),
    onInstall: vi.fn(),
    onUpdate: vi.fn(),
    onSelect,
    onPick: vi.fn(),
    onClose: vi.fn(),
  };
  const { rerender } = render(<Settings {...props} selected="/previous/rzm" />);
  fireEvent.change(screen.getByLabelText("Global executable path"), {
    target: { value: "/typed/rzm" },
  });
  rerender(<Settings {...props} selected="/chosen/rzm" />);
  expect(screen.getByLabelText("Global executable path")).toHaveValue("/chosen/rzm");
  fireEvent.click(screen.getByRole("button", { name: "Use executable" }));
  expect(onSelect).toHaveBeenCalledWith("/chosen/rzm");
});
