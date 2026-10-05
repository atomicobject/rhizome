import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { EnumPillWidget } from "./EnumPillWidget";

const OPTIONS = ["proposed", "active", "superseded", "archived"];

describe("EnumPillWidget", () => {
  it("renders a single highlighted badge in browse mode", () => {
    render(<EnumPillWidget value="active" options={OPTIONS} editing={false} />);
    // Enum values read in sentence case; the raw value stays on data-value.
    expect(screen.getByText("Active")).toHaveAttribute("data-value", "active");
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("stages the selected option when a different segment is clicked", () => {
    const onStage = vi.fn();
    render(<EnumPillWidget value="active" options={OPTIONS} editing={true} onStage={onStage} />);
    const segments = screen.getAllByRole("button");
    expect(segments).toHaveLength(OPTIONS.length);
    expect(screen.getByRole("button", { name: "Active" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Active" }));
    expect(onStage).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText("Superseded"));
    expect(onStage).toHaveBeenCalledWith("superseded");
  });

  it("shows schema labels and the status tone", () => {
    const optionLabels = [
      { value: "active", label: "In flight", tone: "success" },
      { value: "superseded", label: "superseded" },
    ];

    const { rerender } = render(
      <EnumPillWidget
        value="active"
        options={OPTIONS}
        optionLabels={optionLabels}
        editing={false}
      />,
    );

    expect(screen.getByText("In flight").closest(".status-mark")).toHaveClass(
      "status-mark--success",
    );

    rerender(
      <EnumPillWidget
        value="active"
        options={OPTIONS}
        optionLabels={optionLabels}
        editing={true}
      />,
    );
    expect(screen.getByRole("button", { name: "In flight" })).toHaveAttribute(
      "data-value",
      "active",
    );
    // A label that only echoes the raw value is humanized.
    expect(screen.getByRole("button", { name: "Superseded" })).toBeInTheDocument();
  });

  it("flags a value that is not in the schema", () => {
    render(<EnumPillWidget value="legacy" options={OPTIONS} editing={false} />);
    const badge = screen.getByText("legacy");
    expect(badge.className).toContain("widget-enum--unknown");
  });
});
