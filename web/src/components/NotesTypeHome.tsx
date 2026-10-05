import { useMemo } from "react";

import type {
  OntologyNoteListItem,
  OntologyTypeResponse,
  TypeFieldDoc,
  ValidateEnvelope,
} from "../api/types";
import type { OpenMode } from "./useNoteTabs";
import { HomeInfoBand } from "./HomeInfoBand";
import { EnumPillWidget } from "./widgets/EnumPillWidget";
import { validationHealth } from "./validation/validationPresentation";

/**
 * Pick the type's identity-level status field from a TypeDoc — the first enum
 * field whose lowercased name ends with "status". WHY: mirrors the per-note
 * heuristic in `ontologyFieldPicking.ts::pickIdentityFields` and the backend
 * counterpart in `pkg/app/web/ontology.go::pickIdentityStatusField`. Keep all
 * three in lockstep; if you change one, change the others or the card pill,
 * the identity strip, and the server payload will disagree.
 *
 * Operates on `TypeFieldDoc` (schema metadata) rather than `WorkspaceFieldNode`
 * (per-note projection) — that's why the heuristic is duplicated here and not
 * imported from ontologyFieldPicking.
 */
function pickIdentityStatusTypeField(fields: TypeFieldDoc[] | undefined): TypeFieldDoc | null {
  if (!fields) return null;

  for (const field of fields) {
    if (!field.enumValues || field.enumValues.length === 0) continue;
    const name = (field.name || "").toLowerCase();

    if (name.endsWith("status")) return field;
  }

  return null;
}

type Props = {
  typeDetail: OntologyTypeResponse | null;
  onOpenNote: (path: string, mode?: OpenMode) => void;
  onOpenIssues?: () => void;
  validation?: ValidateEnvelope | null;
  issueCount?: number;
  noteIssueCounts?: ReadonlyMap<string, number>;
  loading?: boolean;
  unavailable?: string | null;
  onRetry?: () => void;
};

const NO_COUNTS: ReadonlyMap<string, number> = new Map();

export function NotesTypeHome({
  typeDetail,
  onOpenNote,
  onOpenIssues,
  validation = null,
  issueCount,
  noteIssueCounts = NO_COUNTS,
  loading,
  unavailable,
  onRetry,
}: Props) {
  const typeName = typeDetail?.type?.name || "";
  const label = typeDetail?.type?.pluralLabel || typeDetail?.type?.label || typeName;

  const identityStatusField = useMemo(
    () => pickIdentityStatusTypeField(typeDetail?.type?.fields),
    [typeDetail?.type?.fields],
  );

  const statusPill = (item: OntologyNoteListItem) => {
    const status = (item.identityStatus || "").trim();

    if (!identityStatusField || !status) return null;

    return (
      <span className="ontology-home__row-pill">
        <EnumPillWidget
          value={status}
          options={identityStatusField.enumValues ?? []}
          editing={false}
        />
      </span>
    );
  };

  return (
    <section
      className="ontology-home ontology-home--type ontology-home--body-only"
      aria-label={`${label} home`}
    >
      {unavailable ? (
        <div className="ontology-home__unavailable" role="alert">
          <div>
            <strong>Could not load this note list</strong>
            <p>{unavailable}</p>
          </div>
          {onRetry && (
            <button type="button" onClick={onRetry}>
              Retry
            </button>
          )}
        </div>
      ) : loading ? (
        <div className="ontology-home__hero-loading" role="status">
          Loading type…
        </div>
      ) : (
        <HomeInfoBand
          label={label}
          notes={typeDetail?.notes ?? []}
          noteIssueCounts={noteIssueCounts}
          health={validationHealth(validation)}
          issueCount={issueCount}
          onOpenNote={onOpenNote}
          onOpenIssues={onOpenIssues}
          rowDetail={statusPill}
        />
      )}
    </section>
  );
}
