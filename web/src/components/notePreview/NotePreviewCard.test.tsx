import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { NodePreview } from "../../api/types";
import { NotePreviewCard } from "./NotePreviewCard";

const typedPreview: NodePreview = {
  ref: "docs/spec.md",
  path: "docs/spec.md",
  title: "Preview contract",
  typeName: "ProductSpec",
  typeLabel: "Product spec",
  format: "markdown",
  identifier: "SPEC-0083",
  summary: "Defines the compact note preview.",
  fragmentResolved: true,
  fields: [
    {
      name: "status",
      label: "Status",
      kind: "enum",
      importance: "KEY",
      values: [{ text: "ready" }],
    },
    {
      name: "governingSpecs",
      label: "Governing specs",
      kind: "link",
      importance: "NORMAL",
      values: [{ text: "Workspace shell", target: "docs/shell.md" }],
      truncated: 2,
    },
  ],
  tags: ["experience"],
  updatedAt: 1_789_000_000,
  hasIssues: true,
};

describe("NotePreviewCard", () => {
  it("renders useful typed context without duplicate tags or file metadata", () => {
    render(<NotePreviewCard target={typedPreview.ref} open={vi.fn()} preview={typedPreview} />);

    expect(screen.getByText("Product spec")).toBeInTheDocument();
    expect(screen.getByText("SPEC-0083")).toBeInTheDocument();
    expect(screen.getByLabelText("Status")).toHaveTextContent("ready");
    expect(screen.getByText("Preview contract")).toBeInTheDocument();
    expect(screen.getByText("Defines the compact note preview.")).toBeInTheDocument();
    expect(screen.getByText("Governing specs")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Workspace shell" })).toBeInTheDocument();
    expect(screen.getByText("+2 more")).toBeInTheDocument();
    expect(screen.queryByText("#experience")).not.toBeInTheDocument();
    expect(screen.getByText("Issues")).toBeInTheDocument();
    expect(screen.queryByText("docs/spec.md")).not.toBeInTheDocument();
    expect(screen.getByText("Preview contract")).toHaveAttribute("title", "docs/spec.md");
  });

  it("preserves a schema-selected tags property on a typed preview", () => {
    render(
      <NotePreviewCard
        target={typedPreview.ref}
        open={vi.fn()}
        preview={{
          ...typedPreview,
          fields: [
            ...typedPreview.fields,
            {
              name: "tags",
              label: "Topics",
              kind: "scalar",
              importance: "NORMAL",
              values: [{ text: "experience" }],
            },
          ],
        }}
      />,
    );

    expect(screen.getByText("Topics")).toBeInTheDocument();
    expect(screen.getByText("experience")).toBeInTheDocument();
    expect(screen.queryByText("#experience")).not.toBeInTheDocument();
  });

  it("uses a sensible label for an untyped note", () => {
    render(
      <NotePreviewCard
        target="notes/plain.md"
        open={vi.fn()}
        preview={{
          ref: "notes/plain.md",
          path: "notes/plain.md",
          title: "Plain note",
          format: "markdown",
          fragmentResolved: true,
          fields: [],
          hasIssues: false,
        }}
      />,
    );

    expect(screen.getByText("Note")).toBeInTheDocument();
    expect(screen.getByText("Plain note")).toBeInTheDocument();
  });

  it("preserves case-variant frontmatter tags when no tag chips represent them", () => {
    render(
      <NotePreviewCard
        target="notes/plain.md"
        open={vi.fn()}
        preview={{
          ref: "notes/plain.md",
          path: "notes/plain.md",
          title: "Plain note",
          format: "markdown",
          fragmentResolved: true,
          fields: [
            {
              name: "Tags",
              label: "Tags",
              kind: "scalar",
              importance: "NORMAL",
              values: [{ text: "research" }],
            },
          ],
          hasIssues: false,
        }}
      />,
    );
    expect(screen.getByText("Tags")).toBeInTheDocument();
    expect(screen.getByText("research")).toBeInTheDocument();
    expect(screen.queryByText("#research")).not.toBeInTheDocument();
  });

  it("renders a matched fragment as a secondary title", () => {
    render(
      <NotePreviewCard
        target="notes/plain.md#Some heading"
        open={vi.fn()}
        preview={{
          ref: "notes/plain.md#Some heading",
          path: "notes/plain.md",
          title: "Plain note",
          format: "markdown",
          fragmentResolved: true,
          fragment: { kind: "heading", text: "Some heading" },
          fields: [],
          hasIssues: false,
        }}
      />,
    );

    expect(screen.getByText("Some heading").parentElement).toHaveClass("note-preview__fragment");
    expect(screen.queryByText("Heading not found; showing note.")).not.toBeInTheDocument();
  });

  it("explains an unresolved fragment fallback", () => {
    render(
      <NotePreviewCard
        target="notes/plain.md#missing"
        open={vi.fn()}
        preview={{
          ref: "notes/plain.md",
          path: "notes/plain.md",
          title: "Plain note",
          format: "markdown",
          fragmentResolved: false,
          fields: [],
          hasIssues: false,
        }}
      />,
    );

    expect(screen.getByText("Heading not found; showing note.")).toBeInTheDocument();
  });

  it("renders an error state", () => {
    render(<NotePreviewCard target="missing.md" open={vi.fn()} error />);

    expect(screen.getByText("Preview unavailable for missing.md")).toBeInTheDocument();
  });
});

it.each(["_FallbackNote", "_FallbackSection"])("keeps %s out of preview labels", (typeName) => {
  render(
    <NotePreviewCard
      target={typedPreview.ref}
      open={vi.fn()}
      preview={{ ...typedPreview, typeName, typeLabel: typeName }}
    />,
  );
  expect(screen.getByText("Note")).toBeInTheDocument();
  expect(screen.queryByText(typeName)).not.toBeInTheDocument();
});
