import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { HTMLRootMetadataPanel } from "./HTMLRootMetadataPanel";

describe("HTMLRootMetadataPanel", () => {
  it("stages metadata insertion fields for an untyped HTML note", () => {
    const onStageOps = vi.fn();
    render(
      <HTMLRootMetadataPanel
        path="reports/untyped.html"
        fallbackTitle="Untyped report"
        frontmatter={{}}
        existingFields={new Set()}
        editing
        dirtyFields={new Set()}
        vaultKey="/vault"
        sourceRevision={{
          notePath: "reports/untyped.html",
          contentFingerprint: "html-hash",
          content: "<html>original</html>",
        }}
        onStageOps={onStageOps}
      />,
    );

    const disclosure = screen.getByText("HTML metadata").closest("details");
    expect(disclosure).not.toHaveAttribute("open");
    expect(screen.getByRole("textbox", { name: "type" })).not.toBeVisible();
    fireEvent.click(screen.getByText("HTML metadata"));
    expect(disclosure).toHaveAttribute("open");

    const type = screen.getByRole("textbox", { name: "type" });
    fireEvent.change(type, { target: { value: "Report" } });
    fireEvent.blur(type);
    expect(onStageOps).toHaveBeenCalledWith([
      {
        id: "field:reports%2Funtyped.html:note:type",
        kind: "setField",
        path: "reports/untyped.html",
        field: "type",
        value: "Report",
        fieldValue: { kind: "scalar", scalar: "Report" },
        expected: {
          field: { kind: "unset" },
          sourceHash: "html-hash",
          sourceContent: "<html>original</html>",
        },
      },
    ]);

    const tags = screen.getByRole("textbox", { name: "tags" });
    fireEvent.change(tags, { target: { value: "demo\nreport" } });
    fireEvent.blur(tags);
    expect(onStageOps).toHaveBeenLastCalledWith([
      {
        id: "field:reports%2Funtyped.html:note:tags",
        kind: "setRootMetadataList",
        path: "reports/untyped.html",
        nodeId: undefined,
        field: "tags",
        value: undefined,
        values: ["demo", "report"],
        fieldValue: { kind: "list", items: ["demo", "report"] },
        expected: {
          field: { kind: "unset" },
          sourceHash: "html-hash",
          sourceContent: "<html>original</html>",
        },
      },
    ]);
  });

  it("preserves authored empty lists as present metadata", () => {
    render(
      <HTMLRootMetadataPanel
        path="reports/typed.html"
        fallbackTitle="Typed"
        frontmatter={{ aliases: [] }}
        existingFields={new Set()}
        editing={false}
        dirtyFields={new Set()}
      />,
    );

    expect(screen.getByText("aliases").closest(".ontology-properties__row")).not.toHaveClass(
      "is-missing",
    );
  });

  it("does not duplicate schema-backed title and tags controls", () => {
    render(
      <HTMLRootMetadataPanel
        path="reports/typed.html"
        fallbackTitle="Typed"
        frontmatter={{ type: "Report", title: "Typed", tags: ["demo"], aliases: ["T"] }}
        existingFields={new Set(["title", "tags"])}
        editing={false}
        dirtyFields={new Set()}
      />,
    );
    fireEvent.click(screen.getByText("HTML metadata"));
    expect(screen.queryByText("title")).not.toBeInTheDocument();
    expect(screen.queryByText("tags")).not.toBeInTheDocument();
    expect(screen.getByText("Report")).toBeVisible();
    expect(screen.getByText("T")).toBeVisible();
  });
});
