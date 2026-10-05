import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { NodeFieldCapability, WorkspaceFieldNode } from "../../api/types";
import { withFakeFetch } from "../../test/fakeFetch";
import { renderWithQueryClient as render } from "../../test/renderWithQueryClient";
import {
  clearPersistedEditorDraft,
  EDITOR_REJECTED_EVENT,
  editorDraftStorageKey,
} from "./draftStorage";
import { FieldEditor } from "./FieldEditor";

const http = withFakeFetch();

afterEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
});

function fieldNode(
  name: string,
  values: string[],
  capability: Partial<NodeFieldCapability>,
): WorkspaceFieldNode {
  return {
    id: `field:${name}`,
    kind: "field",
    ref: { notePath: "notes/example.md", kind: "NOTE", typeName: "Example" },
    notePath: "notes/example.md",
    status: {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    },
    capabilities: {
      canEdit: true,
      canEditFields: true,
      canEditCollections: false,
      canNavigateChildren: false,
      canSubscribe: true,
    },
    field: {
      name,
      valueKind: capability.valueKind || "text",
      present: values.length > 0,
      values,
      range: { start: 0, end: 0 },
      capability: {
        ownerRef: { notePath: "notes/example.md", kind: "NOTE", typeName: "Example" },
        ownerType: "Example",
        typeName: "String",
        valueKind: "text",
        list: false,
        required: false,
        enumValues: [],
        valueOrigin: "authored",
        identifier: false,
        preferredIdentifier: false,
        displayImportance: "NORMAL",
        writeOperation: "setField",
        ...capability,
      },
    },
  };
}

describe("FieldEditor", () => {
  it("keeps false distinct from an empty nullable Boolean", () => {
    const onStage = vi.fn();
    render(
      <FieldEditor
        node={fieldNode("enabled", [], { typeName: "Boolean", valueKind: "boolean" })}
        editing
        onStage={onStage}
      />,
    );

    fireEvent.change(screen.getByLabelText("enabled"), { target: { value: "false" } });
    expect(onStage).toHaveBeenCalledWith(
      { kind: "scalar", scalar: "false" },
      { field: { kind: "unset" } },
    );
    fireEvent.change(screen.getByLabelText("enabled"), { target: { value: "" } });
    expect(onStage).toHaveBeenLastCalledWith({ kind: "unset" });
  });

  it("keeps an empty required text draft local and reports the error", () => {
    const onStage = vi.fn();
    render(
      <FieldEditor
        node={fieldNode("summary", ["Existing"], { required: true })}
        editing
        onStage={onStage}
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("summary");
    fireEvent.change(input, { target: { value: "" } });
    fireEvent.blur(input);

    expect(input.value).toBe("");
    expect(onStage).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("summary is required.");
  });

  it("rejects empty required numbers while retaining the local draft", () => {
    const onStage = vi.fn();
    render(
      <FieldEditor
        node={fieldNode("priority", ["2"], { valueKind: "int", typeName: "Int", required: true })}
        editing
        onStage={onStage}
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("priority");
    fireEvent.change(input, { target: { value: "" } });
    fireEvent.blur(input);

    expect(input.value).toBe("");
    expect(onStage).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("priority is required.");
  });

  it("restores an empty required number draft and clears it after acceptance", () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "field-test");
    const operationTarget = "field:test:number";
    const key = editorDraftStorageKey("/vault", operationTarget);
    expect(key).not.toBeNull();
    window.localStorage.setItem(key!, "");
    const onStage = vi.fn();
    render(
      <FieldEditor
        node={fieldNode("priority", ["2"], { valueKind: "int", typeName: "Int", required: true })}
        editing
        vaultKey="/vault"
        operationTarget={operationTarget}
        onStage={onStage}
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("priority");
    expect(input.value).toBe("");
    fireEvent.change(input, { target: { value: "3" } });
    fireEvent.blur(input);

    expect(onStage).toHaveBeenCalledWith(
      { kind: "scalar", scalar: "3" },
      { field: { kind: "scalar", scalar: "2" } },
    );
    expect(window.localStorage.getItem(key!)).toBeNull();
  });

  it("rejects an empty required list without staging an empty collection", () => {
    const onStage = vi.fn();
    render(
      <FieldEditor
        node={fieldNode("tags", ["one"], { list: true, required: true })}
        editing
        onStage={onStage}
      />,
    );
    const input = screen.getByLabelText<HTMLTextAreaElement>("tags");
    fireEvent.change(input, { target: { value: "" } });
    fireEvent.blur(input);

    expect(input).toHaveValue("");
    expect(onStage).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("tags is required.");
  });

  it("rejects empty required relations, enums, and booleans", () => {
    const relationStage = vi.fn();

    const { unmount } = render(
      <FieldEditor
        node={fieldNode("owner", ["notes/owner.md"], {
          valueKind: "relation",
          targetType: undefined,
          required: true,
        })}
        editing
        onStage={relationStage}
      />,
    );

    fireEvent.change(screen.getByLabelText("owner"), { target: { value: "" } });
    expect(relationStage).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("owner is required.");
    unmount();

    const enumStage = vi.fn();
    render(
      <FieldEditor
        node={fieldNode("status", ["active"], {
          valueKind: "enum",
          enumValues: ["active", "done"],
          required: true,
        })}
        editing
        onStage={enumStage}
      />,
    );
    expect(screen.queryByRole("button", { name: "Clear status" })).not.toBeInTheDocument();
    expect(enumStage).not.toHaveBeenCalled();

    const boolStage = vi.fn();

    const boolNode = fieldNode("enabled", ["true"], {
      typeName: "Boolean",
      valueKind: "boolean",
      required: true,
    });

    render(<FieldEditor node={boolNode} editing onStage={boolStage} />);
    fireEvent.change(screen.getByLabelText("enabled"), { target: { value: "" } });
    expect(boolStage).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("enabled is required.");
  });

  it.each(["date", "datetime"] as const)(
    "rejects an empty required %s value without staging it",
    (kind) => {
      const onStage = vi.fn();
      render(
        <FieldEditor
          node={fieldNode("due", [kind === "date" ? "2026-04-12" : "2026-04-12T14:30:17Z"], {
            valueKind: kind,
            typeName: kind === "date" ? "Date" : "DateTime",
            required: true,
          })}
          editing
          onStage={onStage}
        />,
      );
      const input = screen.getByLabelText<HTMLInputElement>("due");
      fireEvent.change(input, { target: { value: "" } });
      fireEvent.blur(input);

      expect(input).toHaveValue("");
      expect(onStage).not.toHaveBeenCalled();
      expect(screen.getByRole("alert")).toHaveTextContent("due is required.");
    },
  );

  it("edits scalar lists without flattening them into one value", () => {
    const onStage = vi.fn();
    render(
      <FieldEditor node={fieldNode("tags", ["one"], { list: true })} editing onStage={onStage} />,
    );

    const input = screen.getByLabelText("tags");
    fireEvent.change(input, { target: { value: "one\ntwo" } });
    fireEvent.blur(input);
    expect(onStage).toHaveBeenCalledWith(
      { kind: "list", items: ["one", "two"] },
      { field: { kind: "list", items: ["one"] } },
    );
  });

  it("explains schema-owned read-only fields", () => {
    render(
      <FieldEditor
        node={fieldNode("backlinks", ["notes/one.md"], {
          valueKind: "relation",
          writeOperation: undefined,
          readOnlyReason: "This relation is computed from neighboring nodes.",
        })}
        editing
        onStage={vi.fn()}
      />,
    );

    expect(screen.getByText("This relation is computed from neighboring nodes.")).toBeVisible();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("filters relation choices to the declared target type", async () => {
    http.json("GET", "/api/v1/ontology/types/Spec", {
      count: 1,
      notes: [
        {
          ref: { notePath: "specs/one.md", kind: "NOTE" },
          path: "specs/one.md",
          title: "Spec One",
          hasIssues: false,
        },
      ],
    });
    const onStage = vi.fn();
    render(
      <FieldEditor
        node={fieldNode("spec", [], {
          typeName: "Spec",
          valueKind: "relation",
          targetType: "Spec",
          writeOperation: "setLinkField",
        })}
        editing
        onStage={onStage}
      />,
    );

    await waitFor(() => expect(screen.getByRole("option", { name: "Spec One" })).toBeVisible());
    fireEvent.change(screen.getByLabelText("spec"), { target: { value: "[[specs/one]]" } });
    expect(onStage).toHaveBeenCalledWith(
      { kind: "scalar", scalar: "[[specs/one]]" },
      { field: { kind: "unset" } },
    );
  });

  it("selects the exact relation target over a same-named file elsewhere", async () => {
    const note = (path: string, title: string) => ({
      ref: { notePath: path, kind: "NOTE" },
      path,
      title,
      hasIssues: false,
    });

    http.json("GET", "/api/v1/ontology/types/Task", {
      count: 2,
      notes: [note("archive/task.md", "Archived task"), note("task.md", "Current task")],
    });
    render(
      <FieldEditor
        node={fieldNode("task", ["[[task]]"], {
          typeName: "Task",
          valueKind: "relation",
          targetType: "Task",
          writeOperation: "setLinkField",
        })}
        editing
        onStage={vi.fn()}
      />,
    );

    await waitFor(() => expect(screen.getByRole("option", { name: "Current task" })).toBeVisible());
    expect(screen.getByLabelText("task")).toHaveValue("[[task]]");
  });

  it("reorders relation lists without changing their members", () => {
    const onStage = vi.fn();
    render(
      <FieldEditor
        node={fieldNode("specs", ["specs/one.md", "specs/two.md"], {
          list: true,
          typeName: "Spec",
          valueKind: "relation",
          writeOperation: "setLinkField",
        })}
        editing
        onStage={onStage}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Move specs/two up" }));
    expect(onStage).toHaveBeenCalledWith(
      {
        kind: "list",
        items: ["specs/two.md", "specs/one.md"],
      },
      { field: { kind: "list", items: ["specs/one.md", "specs/two.md"] } },
    );
  });

  it("shows relation values by target title, unresolved ones without link syntax", () => {
    const node = fieldNode("reviewers", ["[[people/a-lovelace]]", "[[people/missing|Missing]]"], {
      typeName: "Person",
      valueKind: "relation",
      list: true,
    });

    node.field.links = [
      {
        value: "[[people/a-lovelace]]",
        title: "Ada Lovelace",
        ref: { notePath: "people/a-lovelace.md", kind: "NOTE" },
      },
      { value: "[[people/missing|Missing]]" },
    ];

    const editable = render(<FieldEditor node={node} editing={false} onStage={vi.fn()} />);
    expect(editable.container).toHaveTextContent(/^Ada Lovelace, Missing$/);
    editable.unmount();

    const readOnly = render(
      <FieldEditor
        node={{
          ...node,
          field: {
            ...node.field,
            capability: node.field.capability && {
              ...node.field.capability,
              writeOperation: undefined,
            },
          },
        }}
        editing={false}
        onStage={vi.fn()}
      />,
    );

    expect(readOnly.container).toHaveTextContent(/^Ada Lovelace, Missing$/);
  });

  it("lets optional enum fields be unset", () => {
    const onStage = vi.fn();
    render(
      <FieldEditor
        node={fieldNode("status", ["active"], {
          valueKind: "enum",
          enumValues: ["active", "done"],
        })}
        editing
        onStage={onStage}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Clear status" }));
    expect(onStage).toHaveBeenCalledWith(
      { kind: "unset" },
      { field: { kind: "scalar", scalar: "active" } },
    );
  });

  it("keeps the original witness when a dirty field rerenders", () => {
    const onStage = vi.fn();
    const original = fieldNode("summary", ["before"], {});
    const updated = fieldNode("summary", ["external"], {});

    const { rerender } = render(
      <FieldEditor
        node={original}
        editing
        expected={{ sourceHash: "source-before", sourceContent: "before" }}
        onStage={onStage}
      />,
    );

    const input = screen.getByLabelText<HTMLInputElement>("summary");
    input.focus();
    fireEvent.change(input, { target: { value: "draft" } });
    rerender(
      <FieldEditor
        node={updated}
        editing
        expected={{ sourceHash: "source-external", sourceContent: "external" }}
        onStage={onStage}
      />,
    );
    fireEvent.blur(input);

    expect(onStage).toHaveBeenCalledWith(
      { kind: "scalar", scalar: "draft" },
      {
        field: { kind: "scalar", scalar: "before" },
        sourceHash: "source-before",
        sourceContent: "before",
      },
    );
  });

  it("retains its persisted draft when an asynchronous stage fails", async () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "field-test");
    const operationTarget = "field:test:summary";
    const onStage = vi.fn(() => Promise.reject(new Error("offline")));
    render(
      <FieldEditor
        node={fieldNode("summary", ["before"], {})}
        editing
        vaultKey="/vault"
        operationTarget={operationTarget}
        onStage={onStage}
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("summary");
    fireEvent.change(input, { target: { value: "recover me" } });
    fireEvent.blur(input);

    const key = editorDraftStorageKey("/vault", operationTarget);
    await waitFor(() => expect(onStage).toHaveBeenCalledTimes(1));
    expect(key).not.toBeNull();
    expect(window.localStorage.getItem(key || "")).toContain("recover me");
  });

  it("shows the saved value again when the server refuses its edit", async () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "field-test");
    const operationTarget = "field:test:summary";
    render(
      <FieldEditor
        node={fieldNode("summary", ["before"], {})}
        editing
        vaultKey="/vault"
        operationTarget={operationTarget}
        onStage={() => Promise.reject(new Error("refused"))}
      />,
    );
    const input = screen.getByLabelText<HTMLInputElement>("summary");
    fireEvent.change(input, { target: { value: "refused value" } });
    fireEvent.blur(input);

    // The edit session clears the draft and announces the refusal.
    clearPersistedEditorDraft("/vault", operationTarget);
    act(() => {
      window.dispatchEvent(new CustomEvent(EDITOR_REJECTED_EVENT, { detail: { operationTarget } }));
    });

    await waitFor(() => expect(screen.getByLabelText("summary")).toHaveValue("before"));
  });

  it("captures a fresh witness after a successful transfer", () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "field-test");
    const onStage = vi.fn();
    const operationTarget = "field:test:summary";

    const { rerender } = render(
      <FieldEditor
        node={fieldNode("summary", ["alpha"], {})}
        editing
        vaultKey="/vault"
        operationTarget={operationTarget}
        expected={{ sourceHash: "hash-alpha", sourceContent: "alpha" }}
        onStage={onStage}
      />,
    );

    let input = screen.getByLabelText<HTMLInputElement>("summary");
    fireEvent.change(input, { target: { value: "bravo" } });
    fireEvent.blur(input);

    expect(onStage).toHaveBeenLastCalledWith(
      { kind: "scalar", scalar: "bravo" },
      {
        field: { kind: "scalar", scalar: "alpha" },
        sourceHash: "hash-alpha",
        sourceContent: "alpha",
      },
    );

    rerender(
      <FieldEditor
        node={fieldNode("summary", ["bravo"], {})}
        editing
        vaultKey="/vault"
        operationTarget={operationTarget}
        expected={{ sourceHash: "hash-bravo", sourceContent: "bravo" }}
        onStage={onStage}
      />,
    );
    input = screen.getByLabelText<HTMLInputElement>("summary");
    fireEvent.change(input, { target: { value: "charlie" } });
    fireEvent.blur(input);

    expect(onStage).toHaveBeenLastCalledWith(
      { kind: "scalar", scalar: "charlie" },
      {
        field: { kind: "scalar", scalar: "bravo" },
        sourceHash: "hash-bravo",
        sourceContent: "bravo",
      },
    );
  });

  it("captures a fresh witness after a local draft is canceled", () => {
    window.sessionStorage.setItem("rhizome:ontology-edit-session:tab-id", "field-test");
    const onStage = vi.fn();
    const operationTarget = "field:test:summary";

    const { rerender } = render(
      <FieldEditor
        node={fieldNode("summary", ["alpha"], {})}
        editing
        vaultKey="/vault"
        operationTarget={operationTarget}
        expected={{ sourceHash: "hash-alpha", sourceContent: "alpha" }}
        onStage={onStage}
      />,
    );

    let input = screen.getByLabelText<HTMLInputElement>("summary");
    input.focus();
    fireEvent.change(input, { target: { value: "local" } });
    fireEvent.keyDown(input, { key: "Escape" });

    rerender(
      <FieldEditor
        node={fieldNode("summary", ["bravo"], {})}
        editing
        vaultKey="/vault"
        operationTarget={operationTarget}
        expected={{ sourceHash: "hash-bravo", sourceContent: "bravo" }}
        onStage={onStage}
      />,
    );
    input = screen.getByLabelText<HTMLInputElement>("summary");
    fireEvent.change(input, { target: { value: "charlie" } });
    fireEvent.blur(input);

    expect(onStage).toHaveBeenLastCalledWith(
      { kind: "scalar", scalar: "charlie" },
      {
        field: { kind: "scalar", scalar: "bravo" },
        sourceHash: "hash-bravo",
        sourceContent: "bravo",
      },
    );
  });

  it("keeps a focused control mounted when the field becomes dirty", () => {
    const node = fieldNode("enabled", ["false"], {
      valueKind: "boolean",
      typeName: "Boolean",
    });

    const { rerender } = render(<FieldEditor node={node} editing onStage={vi.fn()} />);
    const select = screen.getByLabelText<HTMLSelectElement>("enabled");
    select.focus();
    expect(select).toHaveFocus();
    fireEvent.change(select, { target: { value: "true" } });
    rerender(<FieldEditor node={node} editing dirty onStage={vi.fn()} />);

    expect(screen.getByLabelText("enabled")).toBe(select);
    expect(select).toHaveFocus();
    expect(screen.getByLabelText<HTMLSelectElement>("enabled").value).toBe("true");
  });

  it("fails closed when schema edit metadata is absent", () => {
    const node = fieldNode("summary", ["Text"], {});
    node.field.capability = undefined;
    render(<FieldEditor node={node} editing onStage={vi.fn()} />);
    expect(screen.getByText("Schema edit metadata is unavailable.")).toBeVisible();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("does not stage untouched invalid numbers or empty list entries on unmount", () => {
    const numberStage = vi.fn();

    const number = render(
      <FieldEditor
        node={fieldNode("estimate", ["not-a-number"], {
          typeName: "Int",
          valueKind: "int",
        })}
        editing={false}
        onStage={numberStage}
      />,
    );

    number.unmount();
    expect(numberStage).not.toHaveBeenCalled();

    const listStage = vi.fn();

    const list = render(
      <FieldEditor
        node={fieldNode("labels", ["alpha", ""], { list: true })}
        editing={false}
        onStage={listStage}
      />,
    );

    list.unmount();
    expect(listStage).not.toHaveBeenCalled();
  });
});
