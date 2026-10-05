import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ListValueEditor } from "./ConfiguredTableNodeEditor";

const options = ["a", "b", "c"].map((value) => ({ value, label: value.toUpperCase() }));

function editor(values: string[], onChange = vi.fn()) {
  return (
    <ListValueEditor
      label="Tags"
      values={values}
      options={options}
      labelFor={(value) => value.toUpperCase()}
      dirty={false}
      onChange={onChange}
      onClose={vi.fn()}
    />
  );
}

describe("ListValueEditor", () => {
  it("compounds edits before the row refetches, then yields to an outside change", () => {
    const onChange = vi.fn();
    const { rerender } = render(editor(["a"], onChange));

    fireEvent.change(screen.getByRole("combobox", { name: "Add Tags" }), {
      target: { value: "b" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Remove A" }));
    expect(onChange).toHaveBeenLastCalledWith(["b"]);

    // The refetch catches up to the first staged edit; the draft still holds.
    rerender(editor(["a", "b"], onChange));
    expect(screen.queryByRole("button", { name: "Remove A" })).toBeNull();

    // A discard restores a value this editor never produced.
    rerender(editor(["c"], onChange));
    expect(screen.getByRole("button", { name: "Remove C" })).toBeInTheDocument();
    fireEvent.change(screen.getByRole("combobox", { name: "Add Tags" }), {
      target: { value: "a" },
    });
    expect(onChange).toHaveBeenLastCalledWith(["c", "a"]);
  });
});
