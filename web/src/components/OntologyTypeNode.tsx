import { Handle, type NodeProps, Position } from "@xyflow/react";
import { Fragment } from "react";
import {
  IMPL_SOURCE_LEFT_HANDLE,
  IMPL_SOURCE_RIGHT_HANDLE,
  type OntologyNodeData,
  relationSourceHandle,
  TYPE_TARGET_LEFT_HANDLE,
  TYPE_TARGET_RIGHT_HANDLE,
} from "../lib/ontologyGraph";
import { typeAccentColor } from "../lib/typeAccent";
import { buildOntologyPath } from "./ontologyRoute";

type Props = NodeProps & { data: OntologyNodeData };

/** The accent is consumed by the stylesheet via `var(--type-accent)`. */
type AccentStyle = React.CSSProperties & { [key: `--${string}`]: string };

/** A type name with line-break chances between its words, so "AcceptanceCriterion" wraps whole. */
function nameWithBreaks(name: string) {
  return name.split(/(?<=[a-z0-9])(?=[A-Z])/).map((word, index) => (
    <Fragment key={`${index}:${word}`}>
      {index > 0 && <wbr />}
      {word}
    </Fragment>
  ));
}

/** The card's tooltip: its description and schema survey, which the compact card leaves out. */
function cardTitle(data: OntologyNodeData) {
  const survey = [
    `${data.scalarFieldCount} ${data.scalarFieldCount === 1 ? "field" : "fields"}`,
    `${data.relationCount} ${data.relationCount === 1 ? "relation" : "relations"}`,
    data.sectionCount > 0
      ? `${data.sectionCount} ${data.sectionCount === 1 ? "section" : "sections"}`
      : "",
    data.implements.length > 0 ? `implements ${data.implements.join(", ")}` : "",
  ].filter(Boolean);

  return [data.name, data.description, survey.join(" · ")].filter(Boolean).join("\n");
}

export function OntologyTypeNode({ data }: Props) {
  const accentStyle: AccentStyle = {
    "--type-accent": typeAccentColor(data.name),
  };

  const href = buildOntologyPath({ kind: "type", name: data.name });
  const schemaEdges = data.fields.filter((f) => f.isSchemaEdge);

  const roleLabel =
    data.role === "interface"
      ? "Interface"
      : data.role === "section"
        ? "Section"
        : data.role === "embedded"
          ? "Embedded"
          : null;

  const handleTypeLinkClick = (event: React.MouseEvent<HTMLAnchorElement>) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();

    if (window.location.pathname !== href) {
      window.history.pushState({}, "", href);
      window.dispatchEvent(new PopStateEvent("popstate"));
    }
  };

  return (
    <div
      className={`ontology-node${data.role === "interface" ? " ontology-node--interface" : ""}`}
      style={accentStyle}
    >
      <Handle
        type="target"
        position={Position.Left}
        id={TYPE_TARGET_LEFT_HANDLE}
        className="ontology-node__handle ontology-node__handle--target"
      />
      <Handle
        type="target"
        position={Position.Right}
        id={TYPE_TARGET_RIGHT_HANDLE}
        className="ontology-node__handle ontology-node__handle--target"
      />
      <Handle
        type="source"
        position={Position.Left}
        id={IMPL_SOURCE_LEFT_HANDLE}
        className="ontology-node__handle ontology-node__handle--impl"
      />
      <Handle
        type="source"
        position={Position.Right}
        id={IMPL_SOURCE_RIGHT_HANDLE}
        className="ontology-node__handle ontology-node__handle--impl"
      />
      <a
        href={href}
        className="ontology-node__card"
        onClick={handleTypeLinkClick}
        title={cardTitle(data)}
      >
        <span className="ontology-node__name">{nameWithBreaks(data.name)}</span>
        <span className="ontology-node__meta">
          {data.role === "interface"
            ? "Interface"
            : `${(data.count ?? 0).toLocaleString("en-US")} ${
                data.role === "embedded" || data.role === "section"
                  ? (roleLabel ?? "").toLowerCase()
                  : data.count === 1
                    ? "record"
                    : "records"
              }`}
          {data.issueCount > 0 ? (
            <span className="ontology-node__issues">
              {" · "}
              {data.issueCount} issue{data.issueCount === 1 ? "" : "s"}
            </span>
          ) : null}
        </span>
      </a>
      {schemaEdges.map((f) => (
        <Fragment key={`handle-${f.name}`}>
          <Handle
            type="source"
            position={Position.Left}
            id={relationSourceHandle(f.name, "left")}
            className="ontology-node__handle ontology-node__handle--source"
          />
          <Handle
            type="source"
            position={Position.Right}
            id={relationSourceHandle(f.name, "right")}
            className="ontology-node__handle ontology-node__handle--source"
          />
        </Fragment>
      ))}
    </div>
  );
}
