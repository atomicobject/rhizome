import type { AgentSession } from "../../api/types";
import { HARNESS_LABELS } from "./chatAgent";

const formatter = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });

const formatTime = (value?: string) => (value ? formatter.format(new Date(value)) : "");

type Props = {
  sessions: AgentSession[];
  selectedID: string;
  loading: boolean;
  error: boolean;
  deleteConfirmID: string;
  onSelect: (id: string) => void;
  onNew: () => void;
  onDelete: (id: string) => void;
  onRetry: () => void;
};

export function AgentSessionRail({
  sessions,
  selectedID,
  loading,
  error,
  deleteConfirmID,
  onSelect,
  onNew,
  onDelete,
  onRetry,
}: Props) {
  return (
    <aside className="agent-workspace__rail" aria-label="Agent sessions">
      <div className="agent-workspace__rail-head">
        <h1>Agent</h1>
        <button type="button" className="button" onClick={onNew}>
          New session
        </button>
      </div>
      <div className="agent-session-list">
        {loading ? (
          <p className="agent-muted" role="status">
            Loading sessions…
          </p>
        ) : error ? (
          <p className="agent-error" role="alert">
            Could not load sessions.{" "}
            <button type="button" onClick={onRetry}>
              Retry
            </button>
          </p>
        ) : sessions.length === 0 ? (
          <p className="agent-muted">No sessions yet.</p>
        ) : (
          sessions.map((session) => (
            <div
              key={session.id}
              className={session.id === selectedID ? "agent-session is-active" : "agent-session"}
            >
              <button
                type="button"
                className="agent-session__open"
                aria-label={`${session.title || "New chat"} ${session.harness ? HARNESS_LABELS[session.harness] : "local"} ${formatTime(session.updatedAt)}`}
                onClick={() => onSelect(session.id)}
              >
                <span>{session.title || "New chat"}</span>
                <small>
                  {session.harness ? HARNESS_LABELS[session.harness] : "local"}{" "}
                  {formatTime(session.updatedAt)}
                </small>
              </button>
              <button
                type="button"
                className={
                  deleteConfirmID === session.id
                    ? "agent-session__delete is-confirming"
                    : "agent-session__delete"
                }
                aria-label={
                  deleteConfirmID === session.id
                    ? `Confirm delete ${session.title || "New chat"}`
                    : `Delete ${session.title || "New chat"}`
                }
                onClick={(event) => {
                  event.stopPropagation();
                  onDelete(session.id);
                }}
              >
                {deleteConfirmID === session.id ? (
                  "Confirm"
                ) : (
                  <svg aria-hidden="true" viewBox="0 0 24 24" focusable="false">
                    <path d="M9 3h6l1 2h4v2H4V5h4l1-2Z" />
                    <path d="M6 9h12l-1 12H7L6 9Zm4 2v8h2v-8h-2Zm4 0v8h2v-8h-2Z" />
                  </svg>
                )}
              </button>
            </div>
          ))
        )}
      </div>
    </aside>
  );
}
