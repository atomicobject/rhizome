import { useMemo, useState } from "react";

import type { AgentApprovalDecision, AgentEvent, AgentViewContext } from "../../api/types";
import { humanizeName } from "../../lib/labels";
import {
  approvalOutcomes,
  type ApprovalOutcome,
  buildGroups,
  streamedText,
} from "../agent/AgentTimeline";
import type { AgentMessage } from "../../api/types";
import { AssistantMarkdown } from "../markdown/AssistantMarkdown";
import { CONTEXT_KIND_LABELS, sameContext } from "./viewContext";

export function Icon({ d, strokeWidth = 1.4 }: { d: string; strokeWidth?: number }) {
  return (
    <svg
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
    >
      <path d={d} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function ContextChip({
  context,
  onRemove,
}: {
  context: AgentViewContext;
  onRemove?: () => void;
}) {
  return (
    <span className="chat-context" title={context.path ?? context.detail ?? context.route}>
      <span className="chat-context__kind">{CONTEXT_KIND_LABELS[context.kind]}</span>
      <span className="chat-context__label">{context.title}</span>
      {onRemove ? (
        <button type="button" aria-label="Don't attach what I'm looking at" onClick={onRemove}>
          ×
        </button>
      ) : null}
    </span>
  );
}

type ActivityIcon = "edit" | "read" | "run" | "tool" | "denied";

const ACTIVITY_ICONS: Record<ActivityIcon, string> = {
  edit: "m3.5 8.5 3 3 6-7",
  read: "M3 3.5h10M3 6.5h10M3 9.5h6M3 12.5h4",
  run: "m3.5 5 3 3-3 3M8 11.5h4.5",
  tool: "M9.5 3.5a3 3 0 0 0-3.4 4L3 10.6V13h2.4l3.1-3.1a3 3 0 0 0 4-3.4l-1.8 1.8-1.9-.6-.6-1.9z",
  denied: "M4.5 4.5l7 7M11.5 4.5l-7 7",
};

function Activity({
  icon,
  text,
  note,
  failed = false,
}: {
  icon: ActivityIcon;
  text: string;
  note?: string;
  failed?: boolean;
}) {
  return (
    <div
      className={`chat-activity chat-activity--${icon}${failed ? " is-failed" : ""}`}
      title={note}
    >
      <Icon d={ACTIVITY_ICONS[icon]} strokeWidth={icon === "edit" ? 1.6 : 1.4} />
      <span>{text}</span>
    </div>
  );
}

const basename = (path: string) => path.split("/").pop() || path;

/** The command a harness ran, without the login shell it wrapped it in. */
export function displayCommand(command: string): string {
  const wrapped = /^\/bin\/(?:ba|z)?sh -lc (['"])([\s\S]*)\1$/.exec(command);

  return wrapped?.[2] ?? command;
}

function isPageTool(name: string) {
  return name === "page_text" || name.endsWith("page_text");
}

function approvalTitle(event: AgentEvent) {
  return event.result?.name || event.toolName || "An action needs your approval";
}

function ApprovalCard({
  event,
  onDecision,
}: {
  event: AgentEvent;
  onDecision: (requestID: string, decision: AgentApprovalDecision) => Promise<void>;
}) {
  const [submitting, setSubmitting] = useState(false);
  const requestID = event.result?.requestId || "";
  const result = event.result;

  const decide = async (decision: AgentApprovalDecision) => {
    setSubmitting(true);

    try {
      await onDecision(requestID, decision);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <section className="chat-approval" aria-label="Approval requested">
      <strong>{approvalTitle(event)}</strong>
      {result?.reason ? <span>{result.reason}</span> : null}
      {result?.command ? <code>{result.command}</code> : null}
      {result?.paths?.length ? <span>{result.paths.join(", ")}</span> : null}
      <div className="chat-approval__buttons">
        <button
          type="button"
          className="button button--primary"
          disabled={submitting}
          onClick={() => decide("allow")}
        >
          Allow
        </button>
        <button
          type="button"
          className="button"
          disabled={submitting}
          onClick={() => decide("deny")}
        >
          Deny
        </button>
        {result?.allowForSession ? (
          <button
            type="button"
            className="button"
            disabled={submitting}
            onClick={() => decide("allow-for-session")}
          >
            Allow for this chat
          </button>
        ) : null}
      </div>
    </section>
  );
}

function EventLine({
  event,
  outcome,
  autoNote,
  onApproval,
}: {
  event: AgentEvent;
  outcome?: ApprovalOutcome;
  autoNote?: string;
  onApproval: (requestID: string, decision: AgentApprovalDecision) => Promise<void>;
}) {
  const result = event.result;
  const failed = result?.phase === "failed" || (result?.exitCode ?? 0) !== 0;

  switch (event.type) {
    case "command-execution":
      return (
        <Activity
          icon="run"
          text={result?.command ? `Ran ${displayCommand(result.command)}` : "Ran a command"}
          note={[result?.cwd, failed ? `Exit ${result?.exitCode ?? "error"}` : autoNote]
            .filter(Boolean)
            .join(" · ")}
          failed={failed}
        />
      );
    case "file-change": {
      const paths = result?.paths ?? [];

      return (
        <Activity
          icon="edit"
          text={`Edited ${paths.length ? paths.map(basename).join(", ") : result?.name || "files"}`}
          note={[paths.join("\n"), autoNote].filter(Boolean).join("\n")}
          failed={failed}
        />
      );
    }

    case "tool-call": {
      const name = result?.name || event.toolName || "a tool";

      return isPageTool(name) ? (
        <Activity icon="read" text="Read the page" failed={failed} />
      ) : (
        <Activity icon="tool" text={`Used ${humanizeName(name)}`} note={autoNote} failed={failed} />
      );
    }

    case "approval-requested":
      if (result?.phase === "cancelled" || result?.phase === "declined") return null;

      if (outcome === "cancelled") return null;

      if (outcome)
        return (
          <Activity
            icon={outcome === "deny" ? "denied" : "edit"}
            text={`${outcome === "deny" ? "Denied" : "Allowed"}: ${approvalTitle(event)}`}
            note={outcome === "allow-for-session" ? "Allowed for this chat" : undefined}
          />
        );

      return <ApprovalCard event={event} onDecision={onApproval} />;
    case "error":
      return (
        <p className="chat-error" role="alert">
          {event.content || result?.reason || event.error}
        </p>
      );
    default:
      return null;
  }
}

/** The pane's compact transcript: messages, one line per agent action, and approvals. */
export function ChatTimeline({
  messages,
  events,
  running,
  autoNote,
  onApproval,
}: {
  messages: AgentMessage[];
  events: AgentEvent[];
  running: boolean;
  /** Said on actions that ran without asking, in auto mode. */
  autoNote?: string;
  onApproval: (requestID: string, decision: AgentApprovalDecision) => Promise<void>;
}) {
  const groups = useMemo(() => buildGroups(messages, events), [events, messages]);
  const outcomes = useMemo(() => approvalOutcomes(events), [events]);
  let previousContext: AgentViewContext | undefined;
  const last = groups.at(-1);
  const streamingLast = last ? streamedText(last) : "";

  return (
    <>
      {groups.map((group) => {
        const deltas = streamedText(group);

        const stored = group.entries.some(
          (entry) => entry.kind === "message" && entry.message.role === "assistant",
        );

        return (
          <div className="chat-turn" key={group.key}>
            {group.entries.map((entry) => {
              if (entry.kind === "event")
                return (
                  <EventLine
                    key={`event-${entry.event.id}`}
                    event={entry.event}
                    outcome={outcomes.get(entry.event.result?.requestId || "")}
                    autoNote={autoNote}
                    onApproval={onApproval}
                  />
                );

              const { message } = entry;

              if (message.role !== "user")
                return (
                  <div
                    key={`message-${message.id}`}
                    className="chat-message chat-message--assistant"
                  >
                    <AssistantMarkdown>{message.content}</AssistantMarkdown>
                  </div>
                );

              // A chip only where the context changes, so a thread about one
              // page doesn't repeat it on every message.
              const changed =
                message.context && !sameContext(message.context, previousContext)
                  ? message.context
                  : undefined;

              previousContext = message.context ?? previousContext;

              return (
                <div key={`message-${message.id}`} className="chat-message chat-message--user">
                  {changed ? (
                    <div className="chat-message__context">
                      <ContextChip context={changed} />
                    </div>
                  ) : null}
                  <p>{message.content}</p>
                </div>
              );
            })}
            {deltas && !stored ? (
              <div className="chat-message chat-message--assistant is-streaming">
                <AssistantMarkdown>{deltas}</AssistantMarkdown>
              </div>
            ) : null}
          </div>
        );
      })}
      {running && !streamingLast ? (
        <div className="chat-activity chat-activity--working">
          <span className="chat-spinner" aria-hidden="true" />
          <span>Working…</span>
        </div>
      ) : null}
    </>
  );
}
