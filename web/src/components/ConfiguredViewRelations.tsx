import { useState } from "react";
import type { ViewTableRow } from "../api/types";
import { publicWorkspaceRef } from "../api/client";
import { enumLabel } from "../lib/labels";
import { relationTargetRow } from "./ConfiguredTableCellReadOnly";
import { NoteLinkPreview } from "./notePreview/NoteLinkPreview";
import { StatusMark, statusWordTone } from "./StatusMark";
import type { OpenMode } from "./useNoteTabs";
import { type RelationFact, relationTitle } from "./viewProfile";

type OpenRow = (row: ViewTableRow, mode?: OpenMode) => void;

export function RelationChips({
  row,
  fact,
  onOpenRow,
  limit,
}: {
  row: ViewTableRow;
  fact: RelationFact;
  onOpenRow: OpenRow;
  limit?: number;
}) {
  const [expanded, setExpanded] = useState(false);
  const shown = limit && !expanded ? fact.values.slice(0, limit) : fact.values;
  const rest = fact.values.length - shown.length;

  return (
    <span className="configured-brief__chips" title={limit ? fact.label : undefined}>
      {shown.map((value, index) => (
        <span key={`${value.value}:${index}`} className="configured-brief__chip">
          <RelationLink row={row} value={value} onOpenRow={onOpenRow} />
        </span>
      ))}
      {limit !== undefined && fact.values.length > limit && (
        <button
          type="button"
          className="configured-brief__relation-more list-more"
          aria-expanded={expanded}
          draggable={false}
          onPointerDown={(event) => event.stopPropagation()}
          onPointerUp={(event) => event.stopPropagation()}
          onKeyDown={(event) => event.stopPropagation()}
          onKeyUp={(event) => event.stopPropagation()}
          onDragStart={(event) => {
            event.preventDefault();
            event.stopPropagation();
          }}
          onClick={(event) => {
            event.stopPropagation();
            setExpanded(!expanded);
          }}
        >
          {expanded ? "Show less" : `Show ${rest} more`}
        </button>
      )}
    </span>
  );
}

export function RelationLink({
  row,
  value,
  onOpenRow,
}: {
  row: ViewTableRow;
  value: RelationFact["values"][number];
  onOpenRow: OpenRow;
}) {
  const title = relationTitle(value);
  const target = value.ref ? publicWorkspaceRef(value.ref) : "";
  const status = value.status && <RelationStatus status={value.status} />;

  if (!target) {
    return (
      <span>
        {status}
        {title}
      </span>
    );
  }

  return (
    // A link inside a draggable card must not start the card's drag.
    <span className="link-secondary" data-configured-relation-link>
      {status}
      <NoteLinkPreview
        target={target}
        from={row.ref.notePath || row.path}
        open={(next, mode) =>
          onOpenRow(
            relationTargetRow(next, next === target ? title : undefined, value.ref),
            mode === "beside" ? "beside" : "activate",
          )
        }
      >
        {title}
      </NoteLinkPreview>
    </span>
  );
}

/** A linked record's lifecycle value: its status mark, named for screen readers and on hover. */
function RelationStatus({
  status,
}: {
  status: NonNullable<RelationFact["values"][number]["status"]>;
}) {
  const label = enumLabel(status.value, status.label);

  return (
    <span className="configured-brief__status" title={label}>
      <StatusMark tone={statusWordTone(label, status.tone)} label="" />
      <span className="sr-only">{label}</span>
    </span>
  );
}
