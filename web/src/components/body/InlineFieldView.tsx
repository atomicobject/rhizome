import { isDateField, isEnumField } from "../ontologyFieldPicking";
import { findFieldNodeForBodyNode } from "./fieldNodeLookup";
import type { BodyRendererProps } from "./registry";

/**
 * Compact view for an inline_field body block. Renders a small chip at the
 * authored source position showing "fieldName · value" so the source order
 * of the document is preserved visually. The property panel above the body
 * shows the same field as a widget; both surfaces exist intentionally — the
 * chip preserves positional context, the panel is the overview. A future
 * schema annotation (e.g. `@hidden`) can suppress inline chips on fields
 * that authors don't want repeated at their source position.
 */
export function InlineFieldView({ block, node, context }: BodyRendererProps) {
  const fieldNode = findFieldNodeForBodyNode(context.workspace.nodes, block.fieldName, node.id);

  if (!fieldNode) return null;
  const values = fieldNode.field.values || [];
  const value = values.join(", ");
  const isLocator = fieldNode.field.name === "locator";
  const display = isLocator && value ? `^${value}` : value || "—";

  const kindClass = isEnumField(fieldNode)
    ? "body-inline-field--enum"
    : isDateField(fieldNode)
      ? "body-inline-field--date"
      : "body-inline-field--text";

  return (
    <span
      className={`body-inline-field ${kindClass}`}
      data-field={fieldNode.field.name}
      title={`${fieldNode.field.name}: ${display || "(empty)"}`}
    >
      <span className="body-inline-field__key">{fieldNode.field.name}</span>
      <span className="body-inline-field__sep">·</span>
      <span className="body-inline-field__value">{display}</span>
    </span>
  );
}
