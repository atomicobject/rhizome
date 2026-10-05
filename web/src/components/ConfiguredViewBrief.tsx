import { useEffect, useRef, useState } from "react";

import type {
  OntologyEditOp,
  OntologyEditSessionResponse,
  TypeProfile,
  ViewFieldCapability,
  ViewTableRow,
} from "../api/types";
import { publicWorkspaceRef } from "../api/client";
import { enumLabel } from "../lib/labels";
import { changedFields } from "../staging/stagedState";
import type { OpenMode } from "./useNoteTabs";
import {
  capabilityLabel,
  cellHasChangedField,
  editOpForCell,
  stagedTargetForRow,
} from "./ConfiguredTableCellHelpers";
import { ChangedCell, relationTargetRow } from "./ConfiguredTableCellReadOnly";
import { moveMenuFocus, usePopoverDismiss } from "./focusManagement";
import { NoteLinkPreview } from "./notePreview/NoteLinkPreview";
import { StatusMark } from "./StatusMark";
import { useTypeLabel } from "./typeLabels";
import {
  type EnumTag,
  initials,
  lifecycleTag,
  recordFacts,
  type RelationFact,
  relationTitle,
  STALE_DAYS,
} from "./viewProfile";

/** Record content a type profile drives (SPEC-0112): board cards and record briefs. */

type OpenRow = (row: ViewTableRow, mode?: OpenMode) => void;

const BRIEF_REVERSE_LIMIT = 4;

/** The middle of a board card under its title; compact cards show only the implementing type. */
export function BoardCardBody({
  row,
  profile,
  capabilities,
  hiddenFields,
  otherLanes = 0,
  stale = false,
  compact = false,
  showType = false,
  onOpenRow,
}: {
  row: ViewTableRow;
  profile: TypeProfile;
  capabilities: ViewFieldCapability[];
  /** Fields the board already shows by position: the column and lane fields. */
  hiddenFields: string[];
  otherLanes?: number;
  stale?: boolean;
  compact?: boolean;
  showType?: boolean;
  onOpenRow: OpenRow;
}) {
  const typeLabel = useTypeLabel();
  const shown = (field: string) => !hiddenFields.includes(field);
  const markers = <CardMarkers stale={stale} otherLanes={otherLanes} />;

  if (compact) {
    return showType || stale || otherLanes > 0 ? (
      <div className="configured-brief__meta">
        {showType && <span className="configured-brief__type">{typeLabel(row.resolvedType)}</span>}
        {markers}
      </div>
    ) : null;
  }

  const facts = recordFacts(row, profile, capabilities);
  const keyText = facts.keyTexts[0];
  const relation = facts.relations.find((fact) => shown(fact.field));
  const tags = facts.orderedTags.filter((tag) => shown(tag.field));
  const age = changedAge(row);

  return (
    <>
      {showType && <span className="configured-brief__type">{typeLabel(row.resolvedType)}</span>}
      {facts.summary && <p className="configured-brief__summary">{facts.summary}</p>}
      {keyText && (
        <p className="configured-brief__key-text">
          <span className="configured-brief__label">{keyText.label}</span> {keyText.value}
        </p>
      )}
      <div className="configured-brief__meta">
        {tags.map((tag) => (
          <StatusMark key={tag.field} tone={tag.tone} label={tag.label} />
        ))}
        {relation && <RelationChips row={row} fact={relation} onOpenRow={onOpenRow} limit={2} />}
        {facts.reverse.map((fact) => (
          <span key={fact.field} className="configured-brief__count">
            <b>{fact.count}</b> {fact.label.toLowerCase()}
          </span>
        ))}
        <span className="configured-brief__end">
          {markers}
          <People facts={facts.people} />
          {age}
        </span>
      </div>
    </>
  );
}

function CardMarkers({ stale, otherLanes }: { stale: boolean; otherLanes: number }) {
  return (
    <>
      {stale && (
        <span
          className="configured-brief__stale"
          title={`Active and unchanged for ${STALE_DAYS} days`}
        >
          stale
        </span>
      )}
      {otherLanes > 0 && (
        <span
          className="configured-brief__lanes"
          title={`Also in ${otherLanes} other ${otherLanes === 1 ? "lane" : "lanes"}`}
        >
          +{otherLanes} {otherLanes === 1 ? "lane" : "lanes"}
        </span>
      )}
    </>
  );
}

/** The brief's top line: type label, editable lifecycle and ordered tags, people, Changed. */
export function RecordBriefHeader({
  row,
  profile,
  capabilities,
  editSession,
  onStageOps,
}: {
  row: ViewTableRow;
  profile: TypeProfile;
  capabilities: ViewFieldCapability[];
  editSession?: OntologyEditSessionResponse | null;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
}) {
  const typeLabel = useTypeLabel();
  const facts = recordFacts(row, profile, capabilities);
  const lifecycle = lifecycleTag(row, profile, capabilities);
  const tags = lifecycle ? [lifecycle, ...facts.orderedTags] : facts.orderedTags;

  return (
    <span className="configured-brief__header">
      <span className="configured-brief__type">{typeLabel(row.resolvedType)}</span>
      {tags.map((tag) => (
        <TagMenu
          key={tag.field}
          row={row}
          tag={tag}
          editSession={editSession}
          onStageOps={onStageOps}
        />
      ))}
      <span className="configured-brief__end">
        <People facts={facts.people} />
        {changedAge(row)}
      </span>
    </span>
  );
}

/** The brief under its title: summary, key text callouts, relation chips, reverse lists. */
export function RecordBriefBody({
  row,
  profile,
  capabilities,
  hiddenFields,
  onOpenRow,
}: {
  row: ViewTableRow;
  profile: TypeProfile;
  capabilities: ViewFieldCapability[];
  hiddenFields: string[];
  onOpenRow: OpenRow;
}) {
  const facts = recordFacts(row, profile, capabilities);
  const relations = facts.relations.filter((fact) => !hiddenFields.includes(fact.field));

  return (
    <>
      {facts.summary && (
        <p className="configured-brief__summary configured-brief__summary--brief">
          {facts.summary}
        </p>
      )}
      {facts.keyTexts.map((text) => (
        <div key={text.field} className="configured-brief__callout">
          <span className="configured-brief__label">{text.label}</span>
          <p>{text.value}</p>
        </div>
      ))}
      {(relations.length > 0 || facts.reverse.length > 0) && (
        <dl className="configured-brief__links">
          {relations.map((fact) => (
            <div key={fact.field}>
              <dt>{fact.label}</dt>
              <dd>
                <RelationChips row={row} fact={fact} onOpenRow={onOpenRow} />
              </dd>
            </div>
          ))}
          {facts.reverse.map((fact) => (
            <div key={fact.field}>
              <dt>{fact.label}</dt>
              <dd>
                {fact.values.length > 0 ? (
                  <ul className="configured-brief__reverse">
                    {fact.values.slice(0, BRIEF_REVERSE_LIMIT).map((value, index) => (
                      <li key={`${value.value}:${index}`}>
                        <RelationLink row={row} value={value} onOpenRow={onOpenRow} />
                      </li>
                    ))}
                    {fact.count > Math.min(fact.values.length, BRIEF_REVERSE_LIMIT) && (
                      <li className="configured-brief__more">
                        +{fact.count - Math.min(fact.values.length, BRIEF_REVERSE_LIMIT)} more
                      </li>
                    )}
                  </ul>
                ) : (
                  <span className="configured-brief__count">
                    <b>{fact.count}</b> {fact.label.toLowerCase()}
                  </span>
                )}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </>
  );
}

function changedAge(row: ViewTableRow) {
  return row.updatedAt ? (
    <span className="configured-brief__age">
      <ChangedCell value={row.updatedAt} />
    </span>
  ) : null;
}

function People({ facts }: { facts: RelationFact[] }) {
  const names = facts.flatMap((fact) => fact.values.map(relationTitle));

  return names.map((name, index) => (
    <abbr key={`${name}:${index}`} className="configured-brief__person" title={name}>
      {initials(name)}
    </abbr>
  ));
}

function RelationChips({
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
  const shown = limit ? fact.values.slice(0, limit) : fact.values;
  const rest = fact.values.length - shown.length;

  return (
    <span className="configured-brief__chips" title={limit ? fact.label : undefined}>
      {shown.map((value, index) => (
        <span key={`${value.value}:${index}`} className="configured-brief__chip">
          <RelationLink row={row} value={value} onOpenRow={onOpenRow} />
        </span>
      ))}
      {rest > 0 && <span className="configured-brief__more">+{rest}</span>}
    </span>
  );
}

function RelationLink({
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
    <span data-configured-relation-link>
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
      <StatusMark tone={status.tone} label="" />
      <span className="sr-only">{label}</span>
    </span>
  );
}

/** An enum tag that stages a new value through the edit session, like a table cell edit. */
export function TagMenu({
  row,
  tag,
  editSession,
  onStageOps,
}: {
  row: ViewTableRow;
  tag: EnumTag;
  editSession?: OntologyEditSessionResponse | null;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
}) {
  const [open, setOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ref = useRef<HTMLSpanElement>(null);
  const edit = tag.capability.edit;
  const label = capabilityLabel(tag.capability);
  // Escape closes the menu and returns focus to the tag.
  usePopoverDismiss(open, ref, () => setOpen(false));

  // The menu opens on the current value.
  useEffect(() => {
    if (open) ref.current?.querySelector<HTMLElement>("[aria-checked=true]")?.focus();
  }, [open]);

  const dirty = cellHasChangedField(
    tag.field,
    tag.capability,
    changedFields(editSession, stagedTargetForRow(row)),
  );

  const mark = <StatusMark tone={tag.tone} label={tag.label} />;
  const options = edit?.options ?? tag.capability.enumValues?.map((item) => item.value) ?? [];

  if (!edit || edit.list || !onStageOps || options.length === 0) {
    return <span className="configured-brief__tag">{mark}</span>;
  }

  async function choose(value: string) {
    setOpen(false);
    ref.current?.querySelector<HTMLElement>("[aria-expanded]")?.focus();

    if (!edit || !onStageOps || value === tag.value) return;
    const op = editOpForCell(row, tag.field, edit.field, edit.operation, value, false);

    if (!op) return;
    setError(null);

    try {
      await Promise.resolve(onStageOps([op]));
    } catch (caught) {
      setError(caught instanceof Error && caught.message ? caught.message : "Could not stage");
    }
  }

  return (
    <span className="configured-brief__tag-menu" ref={ref}>
      <button
        type="button"
        className={`configured-brief__tag${dirty ? " is-dirty" : ""}`}
        aria-label={`Edit ${label}`}
        aria-description={tag.label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((current) => !current)}
      >
        {mark}
      </button>
      {open && (
        <span
          className="configured-card__move-menu"
          role="menu"
          aria-label={label}
          onKeyDown={(event) => {
            if (moveMenuFocus(event.currentTarget, event.key)) event.preventDefault();
          }}
        >
          {options.map((value) => {
            const option = tag.capability.enumValues?.find((item) => item.value === value);

            return (
              <button
                key={value}
                type="button"
                role="menuitemradio"
                aria-checked={value === tag.value}
                onClick={() => void choose(value)}
              >
                <StatusMark tone={option?.tone} label={enumLabel(value, option?.label)} />
              </button>
            );
          })}
        </span>
      )}
      {error && (
        <span className="configured-brief__error" role="alert">
          {error}
        </span>
      )}
    </span>
  );
}
