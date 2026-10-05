import { type ReactNode, useMemo } from "react";

import { publicOntologyListItemRef } from "../api/client";
import type { OntologyNoteListItem, ValidationHealth } from "../api/types";
import { displayTitle } from "../lib/labels";
import { useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";
import type { OpenMode } from "./useNoteTabs";

const ROWS = 3;

function formatRelative(timestamp: number | undefined): string {
  if (!timestamp) return "Unknown";
  const millis = timestamp < 1e12 ? timestamp * 1000 : timestamp;
  const diffSeconds = Math.round((Date.now() - millis) / 1000);

  if (diffSeconds < 60) return "just now";
  const minutes = Math.round(diffSeconds / 60);

  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);

  if (hours < 48) return `${hours}h ago`;
  const days = Math.round(hours / 24);

  if (days < 60) return `${days}d ago`;

  return `${Math.round(days / 30)}mo ago`;
}

function top(
  notes: OntologyNoteListItem[],
  score: (note: OntologyNoteListItem) => number,
): OntologyNoteListItem[] {
  return notes
    .filter((note) => score(note) > 0)
    .sort((a, b) => score(b) - score(a))
    .slice(0, ROWS);
}

/** Which notes of this list have validation issues, most issues first. */
export function problemNotes(
  notes: OntologyNoteListItem[],
  noteIssueCounts: ReadonlyMap<string, number>,
): OntologyNoteListItem[] {
  return top(notes, (note) => noteIssueCounts.get(note.path) ?? 0);
}

type Props = {
  label: string;
  notes: OntologyNoteListItem[];
  noteIssueCounts: ReadonlyMap<string, number>;
  health: ValidationHealth;
  /** Total issues behind the Problems link; the link hides when absent or zero. */
  issueCount?: number | null;
  onOpenNote: (path: string, mode?: OpenMode) => void;
  onOpenIssues?: () => void;
  /** Extra per-row detail rendered before the row meta (a status pill, a type name). */
  rowDetail?: (note: OntologyNoteListItem) => ReactNode;
};

function HomeInfoRow({
  item,
  meta,
  detail,
  onOpenNote,
}: {
  item: OntologyNoteListItem;
  meta: string;
  detail?: ReactNode;
  onOpenNote: Props["onOpenNote"];
}) {
  const target = publicOntologyListItemRef(item);

  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target,
    open: (path, mode) => onOpenNote(path, mode === "beside" ? "beside" : "activate"),
  });

  return (
    <li>
      <button
        {...previewTrigger.triggerProps}
        type="button"
        className="ontology-home__row"
        onClick={(event) => {
          previewTrigger.close();
          onOpenNote(target, event.metaKey || event.ctrlKey ? "beside" : "activate");
        }}
      >
        <span className="ontology-home__row-title">{displayTitle(item.title) || item.path}</span>
        {detail}
        <span className="ontology-home__row-meta">{meta}</span>
      </button>
      {previewTrigger.preview}
    </li>
  );
}

/**
 * The compact three-column band above the Home graph: recent work, the most
 * connected notes, and the notes carrying validation problems.
 */
export function HomeInfoBand({
  label,
  notes,
  noteIssueCounts,
  health,
  issueCount,
  onOpenNote,
  onOpenIssues,
  rowDetail,
}: Props) {
  const recent = useMemo(() => top(notes, (note) => note.updatedAt || 0), [notes]);
  const linked = useMemo(() => top(notes, (note) => note.relationCount || 0), [notes]);
  const problems = useMemo(() => problemNotes(notes, noteIssueCounts), [notes, noteIssueCounts]);
  const issuesLoaded = notes.length === 0 || noteIssueCounts.size > 0;

  const row = (item: OntologyNoteListItem, meta: string) => (
    <HomeInfoRow
      key={item.path}
      item={item}
      meta={meta}
      detail={rowDetail?.(item)}
      onOpenNote={onOpenNote}
    />
  );

  const list = (items: ReactNode[], empty: string) =>
    items.length ? (
      <ul className="ontology-home__list">{items}</ul>
    ) : (
      <div className="ontology-home__empty-row">{empty}</div>
    );

  const problemsEmpty =
    health === "never_checked"
      ? "Not checked yet."
      : health === "failed"
        ? "Validation unavailable."
        : issuesLoaded
          ? "No problems in these notes."
          : "Checking…";

  return (
    <div className="ontology-home__band" aria-label={`${label} summary`} role="group">
      <section className="ontology-home__section">
        <header className="ontology-home__card-head">
          <h3>Recently changed</h3>
        </header>
        {list(
          recent.map((item) =>
            row(
              item,
              `${formatRelative(item.updatedAt)}${item.relationCount ? ` · ${item.relationCount} links` : ""}`,
            ),
          ),
          "No modified notes yet.",
        )}
      </section>
      <section className="ontology-home__section">
        <header className="ontology-home__card-head">
          <h3>Most linked</h3>
        </header>
        {list(
          linked.map((item) => row(item, `${item.relationCount} links`)),
          "No outgoing links yet.",
        )}
      </section>
      <section className="ontology-home__section ontology-home__section--problems">
        <header className="ontology-home__card-head">
          <h3>Problems</h3>
          {onOpenIssues && issueCount ? (
            <button type="button" className="ontology-home__link" onClick={onOpenIssues}>
              All {issueCount} {issueCount === 1 ? "issue" : "issues"}
            </button>
          ) : null}
        </header>
        {list(
          problems.map((item) => {
            const count = noteIssueCounts.get(item.path) ?? 0;

            return row(item, `${count} ${count === 1 ? "issue" : "issues"}`);
          }),
          problemsEmpty,
        )}
      </section>
    </div>
  );
}
