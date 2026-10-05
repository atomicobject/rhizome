import { publicTypeName } from "../lib/typeNames";

import type {
  OntologyNoteListItem,
  OntologySummaryResponse,
  OntologyTypeResponse,
  ValidateEnvelope,
} from "../api/types";
import type { OpenMode } from "./useNoteTabs";
import { HomeInfoBand } from "./HomeInfoBand";
import { useTypeLabel } from "./typeLabels";
import { validationHealth } from "./validation/validationPresentation";

type Props = {
  summary: OntologySummaryResponse | null;
  typeDetail: OntologyTypeResponse | null;
  loading?: boolean;
  unavailable?: string | null;
  onRetry?: () => void;
  onOpenNote: (path: string, mode?: OpenMode) => void;
  onOpenIssues: () => void;
  validation?: ValidateEnvelope | null;
  noteIssueCounts?: ReadonlyMap<string, number>;
};

const NO_COUNTS: ReadonlyMap<string, number> = new Map();

function RowType({ name }: { name: string }) {
  return <span className="ontology-home__row-type">{useTypeLabel()(name)}</span>;
}

function typeDetail(item: OntologyNoteListItem) {
  const type = publicTypeName(item.resolvedType);

  return type ? <RowType name={type} /> : null;
}

export function NotesAllHome({
  summary,
  typeDetail: allNotes,
  loading,
  unavailable,
  onRetry,
  onOpenNote,
  onOpenIssues,
  validation = null,
  noteIssueCounts = NO_COUNTS,
}: Props) {
  return (
    <section className="ontology-home ontology-home--all" aria-label="All notes home">
      {unavailable ? (
        <div className="ontology-home__unavailable" role="status">
          <div>
            <strong>Workspace data is unavailable</strong>
            <p>{unavailable} No counts or empty-state conclusions are being shown.</p>
          </div>
          {onRetry && (
            <button type="button" onClick={onRetry}>
              Retry connection
            </button>
          )}
        </div>
      ) : loading ? (
        <div className="ontology-home__hero-loading" role="status">
          Loading workspace data…
        </div>
      ) : (
        <HomeInfoBand
          label="Workspace"
          notes={allNotes?.notes ?? []}
          noteIssueCounts={noteIssueCounts}
          health={validationHealth(validation)}
          issueCount={validation?.snapshot?.issueCount ?? summary?.issueNotes ?? null}
          onOpenNote={onOpenNote}
          onOpenIssues={onOpenIssues}
          rowDetail={typeDetail}
        />
      )}
    </section>
  );
}
