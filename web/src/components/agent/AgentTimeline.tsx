import { useMemo, useState } from "react";
import ReactMarkdown from "react-markdown";
import type { AgentApprovalDecision, AgentEvent, AgentMessage } from "../../api/types";
import { isString, type JsonValue } from "../../api/parse";

const timeFormatter = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });

const formatTime = (value?: string) => (value ? timeFormatter.format(new Date(value)) : "");

function preview(value: JsonValue): string {
  if (value === undefined || value === null) return "";

  if (isString(value)) return value;

  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

type ApprovalOutcome = AgentApprovalDecision | "cancelled";

function approvalDecision(value: string): ApprovalOutcome | undefined {
  switch (value) {
    case "allow":
    case "deny":
    case "allow-for-session":
    case "cancelled":
      return value;
    default:
      return undefined;
  }
}

function coalesceEvents(events: AgentEvent[]): AgentEvent[] {
  const result: AgentEvent[] = [];
  const items = new Map<string, number>();

  for (const event of [...events].sort((left, right) => left.id - right.id)) {
    const itemID = event.result?.itemId;

    const canUpdate =
      itemID && ["command-execution", "file-change", "tool-call", "reasoning"].includes(event.type);

    const key = canUpdate ? `${event.result?.turnId || ""}:${event.type}:${itemID}` : "";
    const index = key ? items.get(key) : undefined;

    if (index === undefined) {
      if (key) items.set(key, result.length);
      result.push(event);
    } else {
      const previous = result[index];
      result[index] = {
        ...previous,
        ...event,
        result: {
          ...previous?.result,
          ...event.result,
          command: event.result?.command || previous?.result?.command,
          paths: event.result?.paths?.length ? event.result.paths : previous?.result?.paths,
          name: event.result?.name || previous?.result?.name,
          input: event.result?.input ?? previous?.result?.input,
        },
      };
    }
  }

  return result;
}

type TimelineEntry =
  | { kind: "message"; message: AgentMessage; at: string }
  | { kind: "event"; event: AgentEvent; at: string };

type TurnGroup = { key: string; turnId?: string; entries: TimelineEntry[] };

function buildGroups(messages: AgentMessage[], events: AgentEvent[]): TurnGroup[] {
  const entries: TimelineEntry[] = [
    ...messages.map((message): TimelineEntry => ({
      kind: "message",
      message,
      at: message.createdAt,
    })),
    ...coalesceEvents(events).map((event): TimelineEntry => ({
      kind: "event",
      event,
      at: event.createdAt,
    })),
  ].sort((left, right) => new Date(left.at).getTime() - new Date(right.at).getTime());

  const groups: TurnGroup[] = [];
  let current: TurnGroup | undefined;

  for (const entry of entries) {
    const turnID = entry.kind === "event" ? entry.event.result?.turnId : undefined;

    if (entry.kind === "message" && entry.message.role === "user") {
      current = { key: `message-${entry.message.id}`, entries: [] };
      groups.push(current);
    }

    if (turnID) {
      const existing = groups.find((group) => group.turnId === turnID);

      if (existing) {
        current = existing;
      } else if (current && !current.turnId) {
        current.turnId = turnID;
        current.key = `turn-${turnID}`;
      } else {
        current = { key: `turn-${turnID}`, turnId: turnID, entries: [] };
        groups.push(current);
      }
    }

    if (!current) {
      current = { key: `entry-${groups.length}`, entries: [] };
      groups.push(current);
    }

    current.entries.push(entry);
  }

  return groups;
}

function DetailItem({ event, label }: { event: AgentEvent; label: string }) {
  const result = event.result;

  return (
    <details className="agent-inline-event">
      <summary>
        <span>
          {label}
          {result?.phase ? ` · ${result.phase}` : ""}
          {result?.exitCode !== undefined ? ` · exit ${result.exitCode}` : ""}
        </span>
        <small>{formatTime(event.createdAt)}</small>
      </summary>
      <div className="agent-inline-event__details">
        {result?.paths?.length ? (
          <section>
            <h3>Paths</h3>
            <p>{result.paths.join(", ")}</p>
          </section>
        ) : null}
        {result?.diff ? (
          <section>
            <h3>Diff</h3>
            <pre>{result.diff}</pre>
          </section>
        ) : null}
        {result?.input !== undefined ? (
          <section>
            <h3>Input</h3>
            <pre>{preview(result.input)}</pre>
          </section>
        ) : null}
        {result?.output !== undefined ? (
          <section>
            <h3>Output</h3>
            <pre>{preview(result.output)}</pre>
            {result?.truncated ? <p>Output truncated.</p> : null}
          </section>
        ) : null}
      </div>
    </details>
  );
}

function ApprovalItem({
  event,
  decision,
  onDecision,
}: {
  event: AgentEvent;
  decision?: ApprovalOutcome;
  onDecision: (requestID: string, decision: AgentApprovalDecision) => Promise<void>;
}) {
  const [submitting, setSubmitting] = useState(false);
  const [localDecision, setLocalDecision] = useState<ApprovalOutcome>();
  const requestID = event.result?.requestId || "";
  const resolvedDecision = decision || localDecision;

  const decide = async (next: AgentApprovalDecision) => {
    setSubmitting(true);

    try {
      await onDecision(requestID, next);
      setLocalDecision(next);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <section className="agent-inline-event" aria-label="Approval requested">
      <div className="agent-inline-event__details">
        <strong>{event.result?.name || event.toolName || "Action requires approval"}</strong>
        {event.result?.command ? <code>{event.result.command}</code> : null}
        {event.result?.paths?.length ? <p>{event.result.paths.join(", ")}</p> : null}
        {event.result?.reason ? <p>{event.result.reason}</p> : null}
        {event.result?.input !== undefined ? <pre>{preview(event.result.input)}</pre> : null}
        <div>
          <button
            type="button"
            disabled={submitting || Boolean(resolvedDecision)}
            onClick={() => decide("allow")}
          >
            Allow
          </button>{" "}
          <button
            type="button"
            disabled={submitting || Boolean(resolvedDecision)}
            onClick={() => decide("deny")}
          >
            Deny
          </button>{" "}
          {event.result?.allowForSession ? (
            <button
              type="button"
              disabled={submitting || Boolean(resolvedDecision)}
              onClick={() => decide("allow-for-session")}
            >
              Allow for session
            </button>
          ) : null}
        </div>
        {resolvedDecision ? <p>Decision: {resolvedDecision}</p> : null}
      </div>
    </section>
  );
}

function EventItem({
  event,
  decisions,
  onApproval,
}: {
  event: AgentEvent;
  decisions: Map<string, ApprovalOutcome>;
  onApproval: (requestID: string, decision: AgentApprovalDecision) => Promise<void>;
}) {
  const result = event.result;

  switch (event.type) {
    case "assistant-text-delta":
    case "message_delta":
    case "approval-decision":
      return null;
    case "turn-started":
      return <div className="agent-muted">Turn started</div>;
    case "turn-completed":
      return <div className="agent-muted">Turn completed</div>;
    case "reasoning":
      return <DetailItem event={event} label="Reasoning" />;
    case "command-execution":
      return <DetailItem event={event} label={result?.command || "Command"} />;
    case "file-change":
      return <DetailItem event={event} label={result?.name || "File change"} />;
    case "tool-call":
      return <DetailItem event={event} label={result?.name || event.toolName || "Tool call"} />;
    case "approval-requested":
      // Terminal phases only feed the decisions map; the original card shows the outcome.
      if (result?.phase === "cancelled" || result?.phase === "declined") return null;

      return (
        <ApprovalItem
          event={event}
          decision={decisions.get(result?.requestId || "")}
          onDecision={onApproval}
        />
      );
    case "token-usage":
      return (
        <footer className="agent-muted">
          Tokens: {result?.usage?.totalTokens ?? 0} total · {result?.usage?.inputTokens ?? 0} in ·{" "}
          {result?.usage?.outputTokens ?? 0} out
        </footer>
      );
    case "interrupt-requested":
      return <div className="agent-muted">Interrupt requested</div>;
    case "error":
      return <p className="agent-error">{event.content || result?.reason || event.error}</p>;
    default:
      return event.type === "diagnostic" ? null : <DetailItem event={event} label={event.type} />;
  }
}

export function AgentTimeline({
  messages,
  events,
  onApproval,
}: {
  messages: AgentMessage[];
  events: AgentEvent[];
  onApproval: (requestID: string, decision: AgentApprovalDecision) => Promise<void>;
}) {
  const groups = useMemo(() => buildGroups(messages, events), [events, messages]);

  const decisions = useMemo(() => {
    const mapped = new Map<string, ApprovalOutcome>();

    for (const event of events) {
      if (event.type === "approval-decision" && event.result?.requestId && event.result.status) {
        const decision = approvalDecision(event.result.status);

        if (decision) mapped.set(event.result.requestId, decision);
      }

      if (
        event.type === "approval-requested" &&
        event.result?.requestId &&
        (event.result.phase === "cancelled" || event.result.phase === "declined")
      ) {
        mapped.set(event.result.requestId, "cancelled");
      }
    }

    return mapped;
  }, [events]);

  if (groups.length === 0) {
    return (
      <div className="agent-empty">
        <h2>Ask against the vault.</h2>
        <p>Rhizome will route the turn through the selected coding-agent harness.</p>
      </div>
    );
  }

  return groups.map((group) => {
    const deltas = group.entries
      .filter(
        (entry) =>
          entry.kind === "event" &&
          (entry.event.type === "assistant-text-delta" || entry.event.type === "message_delta"),
      )
      .map((entry) => (entry.kind === "event" ? entry.event.content || "" : ""))
      .join("");

    const assistantStored = group.entries.some(
      (entry) => entry.kind === "message" && entry.message.role === "assistant",
    );

    return (
      <section
        className="agent-turn"
        key={group.key}
        data-turn-id={group.turnId}
        style={{ display: "flex", flexDirection: "column", gap: "var(--sp-3)" }}
      >
        {group.entries.map((entry) =>
          entry.kind === "message" ? (
            <article
              className={`agent-message agent-message--${entry.message.role}`}
              key={`message-${entry.message.id}`}
            >
              <header>
                <span>{entry.message.role}</span>
                <time>{formatTime(entry.message.createdAt)}</time>
              </header>
              <ReactMarkdown>{entry.message.content}</ReactMarkdown>
            </article>
          ) : (
            <EventItem
              key={`event-${entry.event.id}`}
              event={entry.event}
              decisions={decisions}
              onApproval={onApproval}
            />
          ),
        )}
        {deltas && !assistantStored ? (
          <article
            className="agent-message agent-message--assistant is-working"
            aria-live="polite"
            aria-label="Assistant response"
          >
            <header>
              <span>assistant</span>
              <time>streaming</time>
            </header>
            <ReactMarkdown>{deltas}</ReactMarkdown>
          </article>
        ) : null}
      </section>
    );
  });
}
