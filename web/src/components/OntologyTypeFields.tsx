import "./ontology-refinements.css";
import { publicTypeName } from "../lib/typeNames";
import type React from "react";

import { publicOntologyListItemRef } from "../api/client";
import type { OntologyNoteListItem, TypeFieldDoc } from "../api/types";
import { humanizeName } from "../lib/labels";
import { typeAccentColor } from "../lib/typeAccent";
import { buildNoteWebHref } from "../lib/content";

import { notifyLocationChange } from "./locationStore";
import { useNotePreviewTrigger, type OpenNote } from "./notePreview/NoteLinkPreview";

const openNoteTarget: OpenNote = (target, mode) => {
  const href = buildNoteWebHref(target);

  if (mode === "beside") {
    window.open(href, "_blank", "noopener,noreferrer");

    return;
  }

  window.history.pushState({}, "", href);
  notifyLocationChange();
};

export function TargetChip({
  name,
  list,
  onSelect,
  role,
}: {
  name: string;
  list: boolean;
  onSelect: (name: string) => void;
  role?: string;
}) {
  const accent = typeAccentColor(name);

  return (
    <button
      type="button"
      className="ontology-type-detail__target-chip"
      // SAFETY: React's CSSProperties omits `--*` custom properties; this object sets only one.
      style={{ "--target-accent": accent } as React.CSSProperties}
      onClick={() => onSelect(name)}
      title={role ? `${role}: ${name}` : name}
    >
      <span className="ontology-type-detail__target-dot" aria-hidden="true" />
      <span className="ontology-type-detail__target-label">{humanizeName(name)}</span>
      <code className="ontology-type-detail__ident-secondary">
        {name}
        {list ? "[]" : ""}
      </code>
    </button>
  );
}

/** A relation's direction in lowercase with its arrow: "← inbound", "→ outbound". */
export function directionLabel(direction?: string) {
  if (!direction) return <span className="ontology-type-detail__none">—</span>;
  const value = direction.toLowerCase();

  const arrow =
    value === "inbound" ? "← " : value === "outbound" ? "→ " : value === "both" ? "↔ " : "";

  return `${arrow}${value}`;
}

/**
 * Identifiers and example values inside schema prose: IDs such as
 * EFF-2026-08-05-14-32, file names, paths, camelCase names, and backticked
 * code. Each renders as one unbreakable mono span, so a column never splits
 * one mid-token.
 */
const IDENTIFIER =
  /`[^`]+`|\b[A-Z][A-Z0-9]*-[\w-]*\d[\w-]*|[\w./-]*\.(?:html|md|ya?ml|json|go|tsx?)\b|\b[a-z]+[A-Z]\w*/g;

export function Prose({ text }: { text?: string }) {
  if (!text) return null;
  const parts: React.ReactNode[] = [];
  let last = 0;

  for (const match of text.matchAll(IDENTIFIER)) {
    const index = match.index ?? 0;

    if (index > last) parts.push(text.slice(last, index));
    parts.push(
      <code key={index} className="ontology-type-detail__ident">
        {match[0].replace(/^`|`$/g, "")}
      </code>,
    );
    last = index + match[0].length;
  }

  if (last < text.length) parts.push(text.slice(last));

  return <>{parts}</>;
}

export function FieldTable({ fields }: { fields: TypeFieldDoc[] }) {
  // Source shows only where it differs from the field's name.
  const renamed = fields.some((f) => f.source && f.source !== f.name);

  return (
    <table className="ontology-type-detail__table">
      <thead>
        <tr>
          <th className="ontology-type-detail__col-name">Name</th>
          <th className="ontology-type-detail__col-type">Type</th>
          <th className="ontology-type-detail__col-required">Required</th>
          {renamed ? <th className="ontology-type-detail__col-source">Source</th> : null}
          <th>Description</th>
        </tr>
      </thead>
      <tbody>
        {fields.map((f) => (
          <tr key={f.name}>
            <td>
              <span>{humanizeName(f.name)}</span>{" "}
              {humanizeName(f.name).toLowerCase() !== (f.name ?? "").toLowerCase() ? (
                <code className="ontology-type-detail__ident-secondary">{f.name}</code>
              ) : null}
            </td>
            <td>
              <span>{humanizeName(f.typeName || f.kind || "string")}</span>
              {f.typeName && humanizeName(f.typeName).toLowerCase() !== f.typeName.toLowerCase() ? (
                <code className="ontology-type-detail__ident-secondary">{f.typeName}</code>
              ) : null}
              {f.list ? "[]" : ""}
              {f.enumValues?.length ? (
                <span className="ontology-type-detail__enum-chips">
                  {f.enumValues.map((value) => (
                    <code key={value} className="schema-enum-value-chip">
                      {value}
                    </code>
                  ))}
                </span>
              ) : null}
            </td>
            <td>{f.required ? "yes" : "no"}</td>
            {renamed ? (
              <td className="ontology-type-detail__nowrap">
                {f.source && f.source !== f.name ? (
                  <code>{f.source}</code>
                ) : (
                  <span className="empty-dash">—</span>
                )}
              </td>
            ) : null}
            <td>
              <Prose text={f.description} />
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function ExampleCard({
  note,
  onSelectType,
}: {
  note: OntologyNoteListItem;
  onSelectType: (name: string) => void;
}) {
  const target = publicOntologyListItemRef(note);
  const href = buildNoteWebHref(target);

  const previewTrigger = useNotePreviewTrigger<HTMLAnchorElement>({
    target,
    open: openNoteTarget,
  });

  const resolved = publicTypeName(note.resolvedType);
  const resolvedAccent = resolved ? typeAccentColor(resolved) : undefined;
  const tags = (note.tags ?? []).slice(0, 3);

  return (
    <li className="ontology-type-detail__example-card">
      <a
        {...previewTrigger.triggerProps}
        className="ontology-type-detail__example-title"
        href={href}
        onClick={previewTrigger.close}
      >
        {note.title || note.path}
      </a>
      {previewTrigger.preview}
      <div className="ontology-type-detail__example-meta">
        <code>{note.path}</code>
      </div>
      <div className="ontology-type-detail__example-chips">
        {resolved ? (
          <button
            type="button"
            className="ontology-type-detail__target-chip"
            // SAFETY: React's CSSProperties omits `--*` custom properties; this object sets only one.
            style={
              {
                "--target-accent": resolvedAccent,
              } as React.CSSProperties
            }
            onClick={() => onSelectType(resolved)}
          >
            <span className="ontology-type-detail__target-dot" aria-hidden="true" />
            {resolved}
          </button>
        ) : null}
        {tags.map((tag) => (
          <span key={tag} className="ontology-type-detail__tag-chip">
            #{tag}
          </span>
        ))}
        {note.relationCount ? (
          <span className="ontology-type-detail__example-stat">{note.relationCount} rel</span>
        ) : null}
        {note.hasIssues ? (
          <span className="ontology-type-detail__example-stat ontology-type-detail__example-stat--warn">
            ⚠ issues
          </span>
        ) : null}
      </div>
    </li>
  );
}
