import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { RecordRailPortal, RecordRailProvider, useRecordRailState } from "./recordRail";

function Workspace({ show, selected }: { show: boolean; selected?: boolean }) {
  const rail = useRecordRailState();

  return (
    <RecordRailProvider value={rail.rail}>
      <output aria-label="Pane state">{`${rail.showing}/${rail.selected}`}</output>
      <div aria-label="Slot" ref={rail.setSlot} />
      <RecordRailPortal show={show} selected={selected}>
        <p>Record body</p>
      </RecordRailPortal>
    </RecordRailProvider>
  );
}

const state = () => screen.getByRole("status", { name: "Pane state" });

describe("record slot", () => {
  it("renders the record into the slot and reports a selection by default", () => {
    render(<Workspace show />);

    expect(state()).toHaveTextContent("true/true");
    expect(screen.getByLabelText("Slot")).toHaveTextContent("Record body");
  });

  it("reports no selection while the view passes selected={false}, so the pane stays closed", () => {
    const view = render(<Workspace show selected={false} />);
    expect(state()).toHaveTextContent("true/false");

    view.rerender(<Workspace show selected />);
    expect(state()).toHaveTextContent("true/true");

    view.rerender(<Workspace show={false} selected />);
    expect(state()).toHaveTextContent("false/false");
  });
});
