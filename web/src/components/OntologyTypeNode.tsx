import { Handle, type NodeProps, Position, useViewport } from "@xyflow/react";
import { Fragment } from "react";
import {
  fieldRowMidpointY,
  HEADER_HEIGHT,
  ROW_HEIGHT,
  FIELD_LIST_TOP_PADDING,
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

export function OntologyTypeNode({ data }: Props) {
  const { zoom } = useViewport();

  const accentStyle: AccentStyle = {
    "--type-accent": typeAccentColor(data.name),
    "--atlas-label-size": `${Math.min(28, 14 / zoom)}px`,
    "--atlas-header-height": `${HEADER_HEIGHT}px`,
    "--atlas-row-height": `${ROW_HEIGHT}px`,
    "--atlas-field-padding": `${FIELD_LIST_TOP_PADDING}px`,
  };

  const href = buildOntologyPath({ kind: "type", name: data.name });
  const schemaEdges = data.fields.filter((f) => f.isSchemaEdge);

  const roleLabel =
    data.role === "interface"
      ? "interface"
      : data.role === "section"
        ? "section"
        : data.role === "embedded"
          ? "embedded"
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
      className={`ontology-node${data.role !== "note" ? " ontology-node--interface" : ""}${zoom < 0.65 ? " ontology-node--overview" : ""}`}
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
      <a href={href} className="ontology-node__card" onClick={handleTypeLinkClick}>
        <div className="ontology-node__header">
          <div className="ontology-node__title-row">
            <span className="ontology-node__name" title={data.name}>
              {data.name}
            </span>
            {roleLabel ? (
              <span className="ontology-node__badge">{roleLabel}</span>
            ) : (
              <span className="ontology-node__count">{data.count}</span>
            )}
          </div>
          {data.implements.length > 0 ? (
            <div className="ontology-node__impl">
              <span aria-hidden>↳</span>
              {data.implements.map((iface) => (
                <span key={iface} className="ontology-node__chip">
                  {iface}
                </span>
              ))}
            </div>
          ) : null}
          {data.description ? (
            <p className="ontology-node__description">{data.description}</p>
          ) : null}
          <div className="ontology-node__stats">
            <span title="scalar fields">
              <strong>{data.scalarFieldCount}</strong> fields
            </span>
            <span title="typed relations">
              <strong>{data.relationCount}</strong> rel
            </span>
            {data.sectionCount > 0 ? (
              <span title="narrative sections">
                <strong>{data.sectionCount}</strong> sec
              </span>
            ) : null}
            {data.issueCount > 0 ? (
              <span className="ontology-node__issues">
                {data.issueCount} issue{data.issueCount === 1 ? "" : "s"}
              </span>
            ) : null}
          </div>
        </div>
        <ul className="ontology-node__fields">
          {schemaEdges.map((f) => (
            <li key={f.name} className="ontology-node__field ontology-node__field--relation">
              <span className="ontology-node__field-name">
                {f.required ? "★ " : ""}
                {f.name}
              </span>
              <span className="ontology-node__field-type">
                → {f.typeName}
                {f.list ? "[]" : ""}
              </span>
            </li>
          ))}
          {data.scalarFieldCount > 0 ? (
            <li className="ontology-node__field ontology-node__field--summary">
              {data.scalarFieldCount} field
              {data.scalarFieldCount === 1 ? "" : "s"}
            </li>
          ) : null}
        </ul>
      </a>
      {schemaEdges.map((f, index) => (
        <Fragment key={`handle-${f.name}`}>
          <Handle
            type="source"
            position={Position.Left}
            id={relationSourceHandle(f.name, "left")}
            className="ontology-node__handle ontology-node__handle--source"
            style={{ top: `${fieldRowMidpointY(index)}px` }}
          />
          <Handle
            type="source"
            position={Position.Right}
            id={relationSourceHandle(f.name, "right")}
            className="ontology-node__handle ontology-node__handle--source"
            style={{ top: `${fieldRowMidpointY(index)}px` }}
          />
        </Fragment>
      ))}
    </div>
  );
}
