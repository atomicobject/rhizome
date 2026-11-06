import { ChevronRight } from "lucide-react";
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
  duplicateCount = false,
  loadingMore,
  collapsed,
  collapsedGroups = [],
  stagedCount,
  moved = [],
  hidden = [],
  pager,
  onPage,
}: {
  summary: string;
  duplicateCount?: boolean;
  loadingMore: boolean;
  /** Such as "2 columns collapsed"; empty when nothing is. */
  collapsed: string;
  collapsedGroups?: { key: string; label: string; count: number }[];
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
        <span
          role="status"
          className={duplicateCount ? "configured-view__duplicate-count" : undefined}
        >
          {summary}
        </span>
        {loadingMore && <span role="status">Loading more rows…</span>}
        {collapsedGroups.length > 0
          ? collapsedGroups.map((group) => (
              <button
                type="button"
                className="configured-view__collapsed-link"
                aria-label={`Expand ${group.label}: ${group.count} collapsed`}
                key={group.key}
                onClick={(event) => {
                  event.currentTarget
                    .closest(".configured-view")
                    ?.querySelector<HTMLButtonElement>(
                      `button[data-group-key="${CSS.escape(group.key)}"]`,
                    )
                    ?.click();
                }}
              >
                <ChevronRight size={16} aria-hidden />
                {group.label} {group.count} collapsed
              </button>
            ))
          : collapsed && <span>{collapsed}</span>}
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

/** Names columns moved to Record and discloses why columns are hidden. */
function ColumnNotes({ moved, hidden }: { moved: string[]; hidden: string[] }) {
  if (moved.length === 0 && hidden.length === 0) return null;

  const hiddenNames = hidden.map((note) => note.replace(/ column hidden.*$/, ""));

  const summary = [
    moved.length > 0 ? `${moved.length} ${moved.length === 1 ? "field" : "fields"} in Record` : "",
    hidden.length > 0 ? `Hidden: ${hiddenNames.join(", ")}` : "",
  ]
    .filter(Boolean)
    .join(" · ");

  const compact = [
    moved.length > 0 ? `${moved.length} ${moved.length === 1 ? "field" : "fields"} in Record` : "",
    hidden.length > 0 ? `${hidden.length} hidden` : "",
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <details className="configured-view__column-notes">
      <summary
        title={[
          moved.length > 0 ? `In Record: ${moved.join(", ")}` : "",
          hidden.length > 0 ? `Hidden: ${hiddenNames.join(", ")}` : "",
        ]
          .filter(Boolean)
          .join(". ")}
      >
        <span className="configured-view__column-note-full">{summary}</span>
        <span className="configured-view__column-note-compact">{compact}</span>
        <ChevronRight size={16} aria-hidden />
      </summary>
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
