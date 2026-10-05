import { fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { describe, expect, it } from "vitest";

import { useFocusRegions } from "./focusManagement";

function Regions({ panelHidden = false }: { panelHidden?: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  useFocusRegions(ref, ".rail, .panel, .context", true);

  return (
    <div ref={ref}>
      <nav className="rail">
        <button type="button">Rail first</button>
        <button type="button">Rail second</button>
      </nav>
      <section className="panel" hidden={panelHidden}>
        <button type="button">Panel</button>
      </section>
      <aside className="context">
        <button type="button" tabIndex={-1}>
          Skipped
        </button>
        <button type="button">Context</button>
      </aside>
    </div>
  );
}

const button = (name: string) => screen.getByRole("button", { name });

describe("useFocusRegions", () => {
  it("cycles F6 forward and Shift+F6 back through visible regions", () => {
    render(<Regions />);

    fireEvent.keyDown(window, { key: "F6" });
    expect(button("Rail first")).toHaveFocus();
    fireEvent.keyDown(window, { key: "F6" });
    expect(button("Panel")).toHaveFocus();
    fireEvent.keyDown(window, { key: "F6" });
    expect(button("Context")).toHaveFocus();
    fireEvent.keyDown(window, { key: "F6" });
    expect(button("Rail first")).toHaveFocus();
    fireEvent.keyDown(window, { key: "F6", shiftKey: true });
    expect(button("Context")).toHaveFocus();
  });

  it("returns to the element last focused in a region and skips hidden regions", () => {
    render(<Regions panelHidden />);

    button("Rail second").focus();
    fireEvent.keyDown(window, { key: "F6" });
    expect(button("Context")).toHaveFocus();
    fireEvent.keyDown(window, { key: "F6" });
    expect(button("Rail second")).toHaveFocus();
  });
});
