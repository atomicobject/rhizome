import { isJsonValue, isString, type JsonValue } from "../api/parse";

type ErrorInfo = { message: string; status?: number; code?: string; details?: unknown };

type Page = { offset: number; first: number };

/** A failed view request, with its status, code, and details behind a disclosure. */
export function ViewErrorStatus({ error }: { error: ErrorInfo }) {
  const details = viewErrorDetails(error);

  return (
    <div className="configured-view__status configured-view__status--error" role="alert">
      <span>{error.message}</span>
      {details && (
        <details className="configured-view__error-details">
          <summary>Details</summary>
          <pre>{details}</pre>
        </details>
      )}
    </div>
  );
}

/**
 * The view's footer: the row range, loading and collapsed notes, the staged
 * edit count, and a pager for layouts that page instead of scrolling.
 */
export function ConfiguredViewFooter({
  summary,
  loadingMore,
  collapsed,
  stagedCount,
  moved = [],
  hidden = [],
  pager,
  onPage,
}: {
  summary: string;
  loadingMore: boolean;
  /** Such as "2 columns collapsed"; empty when nothing is. */
  collapsed: string;
  stagedCount: number;
  /** Labels of table columns too narrow to show, which the Record pane shows instead. */
  moved?: string[];
  /** Plain notes on columns hidden because one value fills nearly every row. */
  hidden?: string[];
  pager: (Page & { hasMore: boolean }) | null;
  onPage: (page: Page) => void;
}) {
  return (
    <footer className="configured-view__footer">
      <div className="configured-view__footer-status">
        <span role="status">{summary}</span>
        {loadingMore && <span role="status">Loading more rows…</span>}
        {collapsed && <span>{collapsed}</span>}
        <ColumnNotes moved={moved} hidden={hidden} />
        {stagedCount > 0 && (
          <span className="configured-view__staged-count">
            {stagedCount} staged {stagedCount === 1 ? "change" : "changes"}
          </span>
        )}
      </div>
      {pager && (
        <div className="configured-view__pager">
          <button
            type="button"
            disabled={pager.offset <= 0}
            onClick={() =>
              onPage({ offset: Math.max(0, pager.offset - pager.first), first: pager.first })
            }
          >
            Previous page
          </button>
          <button
            type="button"
            disabled={!pager.hasMore}
            onClick={() => onPage({ offset: pager.offset + pager.first, first: pager.first })}
          >
            Next page
          </button>
        </div>
      )}
    </footer>
  );
}

const plural = (count: number, word: string) => `${count} ${word}${count === 1 ? "" : "s"}`;

/**
 * Columns the table does not show, as one compact disclosure ("2 columns in
 * Record · 1 column hidden"); it opens to name each and say why, so a narrow
 * footer never cuts several explanations at once.
 */
function ColumnNotes({ moved, hidden }: { moved: string[]; hidden: string[] }) {
  if (moved.length === 0 && hidden.length === 0) return null;

  const summary = [
    moved.length > 0 ? `${plural(moved.length, "column")} in Record` : "",
    hidden.length > 0 ? `${plural(hidden.length, "column")} hidden` : "",
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <details className="configured-view__column-notes">
      <summary>{summary}</summary>
      <div className="configured-view__column-notes-body">
        {moved.length > 0 && (
          <p>
            {moved.join(", ")} {moved.length === 1 ? "shows" : "show"} in the Record pane while the
            table is narrow.
          </p>
        )}
        {hidden.map((note) => (
          <p key={note}>{note}. Show it again from Display › Columns.</p>
        ))}
      </div>
    </details>
  );
}

function viewErrorDetails(error: ErrorInfo) {
  const parts: string[] = [];

  if (error.status !== undefined) parts.push(`Status: ${error.status}`);

  if (error.code) parts.push(`Code: ${error.code}`);

  if (error.details !== undefined) {
    const details = isJsonValue(error.details) ? error.details : String(error.details);
    parts.push(`Details:\n${formatViewErrorDetails(details)}`);
  }

  return parts.join("\n\n");
}

function formatViewErrorDetails(details: JsonValue) {
  return isString(details) ? details : JSON.stringify(details, null, 2);
}
