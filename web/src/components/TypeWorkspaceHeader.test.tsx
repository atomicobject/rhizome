import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TypeWorkspaceHeader } from "./TypeWorkspaceHeader";

describe("TypeWorkspaceHeader", () => {
  const baseProps = {
    typeName: "SpecLike",
    label: "SpecLike",
    eyebrow: "Type",
    description: "Specs and similar planning notes.",
    totalNotes: 12,
    issueCount: 1,
    meanRelations: 4.2,
  };

  it("links to the ontology type page when a type name is provided", () => {
    render(<TypeWorkspaceHeader {...baseProps} />);
    expect(screen.getByRole("link", { name: "View ontology type SpecLike" })).toHaveAttribute(
      "href",
      "/ontology/type/SpecLike",
    );
  });

  it("omits the ontology type link when no type name is provided", () => {
    render(<TypeWorkspaceHeader {...baseProps} typeName="" />);
    expect(screen.queryByRole("link", { name: /View ontology type/ })).toBeNull();
  });

  it("renders the shared view selector supplied by the workspace", () => {
    const onSelect = vi.fn();
    render(
      <TypeWorkspaceHeader
        {...baseProps}
        viewSelector={<button onClick={onSelect}>Dashboard</button>}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Dashboard" }));
    expect(onSelect).toHaveBeenCalledOnce();
  });

  it("shows dashes instead of zero counts while the type data is unknown", () => {
    const { rerender } = render(
      <TypeWorkspaceHeader {...baseProps} totalNotes={null} meanRelations={null} />,
    );

    expect(screen.getAllByText("—")).toHaveLength(2);
    expect(screen.queryByText("0")).toBeNull();

    rerender(<TypeWorkspaceHeader {...baseProps} />);
    expect(screen.queryByText("—")).toBeNull();
    expect(screen.getByText("12")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
    expect(screen.getByText("4.2")).toBeInTheDocument();
  });

  it("leaves note and link stats to a display group's own view", () => {
    render(
      <TypeWorkspaceHeader
        {...baseProps}
        typeName=""
        eyebrow="Display group"
        totalNotes={null}
        meanRelations={null}
        showStats={false}
      />,
    );

    expect(screen.queryByText("—")).toBeNull();
    expect(screen.queryByText("notes")).toBeNull();
    expect(screen.queryByText("avg links")).toBeNull();
  });

  it("keeps the collection issues action when a native view replaces the stats", () => {
    const onOpenIssues = vi.fn();

    const { rerender } = render(
      <TypeWorkspaceHeader
        {...baseProps}
        issueCount={3}
        showStats={false}
        onOpenIssues={onOpenIssues}
      />,
    );

    expect(screen.queryByText("notes")).toBeNull();
    expect(screen.queryByText("avg links")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Validation issues in SpecLike/ }));
    expect(onOpenIssues).toHaveBeenCalledOnce();

    rerender(<TypeWorkspaceHeader {...baseProps} issueCount={0} showStats={false} />);
    expect(screen.queryByRole("button", { name: /Validation issues/ })).toBeNull();
  });
});
