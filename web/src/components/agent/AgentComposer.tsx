import { interruptAgentSession } from "../../api/internalAgentClient";
import { useLayoutEffect, type RefObject } from "react";
import type { AgentHarnessKind, AgentHarnessStatus } from "../../api/types";
import { AgentAvailability } from "./AgentStatusHeader";

export function AgentComposer({
  runningID,
  onError,
  draft,
  onChange,
  onSubmit,
  composerRef,
  busy,
  blocked,
  readOnly,
  disabledReason,
  harness,
  status,
}: {
  runningID: string;
  onError: (message: string) => void;
  draft: string;
  onChange: (text: string) => void;
  onSubmit: (text: string) => void;
  composerRef: RefObject<HTMLTextAreaElement | null>;
  busy: boolean;
  blocked: boolean;
  readOnly: boolean;
  disabledReason: string;
  harness: AgentHarnessKind | undefined;
  status: AgentHarnessStatus | undefined;
}) {
  const canSend = !busy && !blocked && Boolean(draft.trim()) && !readOnly;
  // The composer grows with its message; CSS caps it, then it scrolls.
  useLayoutEffect(() => {
    const field = composerRef.current;

    if (!field) return;
    field.style.height = "auto";
    field.style.height = `${field.scrollHeight}px`;
  }, [draft, composerRef]);

  return (
    <>
      {runningID ? (
        <button
          type="button"
          onClick={async () => {
            try {
              await interruptAgentSession(runningID);
            } catch (cause) {
              onError(cause instanceof Error ? cause.message : String(cause));
            }
          }}
        >
          Interrupt
        </button>
      ) : null}
      <form
        className="agent-composer"
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit(draft);
        }}
      >
        <div className="agent-composer__frame">
          <textarea
            ref={composerRef}
            value={draft}
            aria-label="Message"
            placeholder="Ask Rhizome…"
            rows={3}
            disabled={busy || readOnly}
            onChange={(event) => onChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                event.preventDefault();
                onSubmit(draft);
              }
            }}
          />
          <div className="agent-composer__bar">
            {disabledReason ? (
              <p className="agent-muted">{disabledReason}</p>
            ) : blocked || status?.loggedIn === false ? (
              <AgentAvailability harness={harness} status={status} />
            ) : (
              <p className="agent-muted">
                <kbd>{/Mac/i.test(navigator.platform) ? "⌘" : "Ctrl"}</kbd> <kbd>↵</kbd> to send
              </p>
            )}
            <button
              type="submit"
              className={`button${canSend ? " button--primary" : ""}`}
              disabled={!canSend}
            >
              Send
            </button>
          </div>
        </div>
      </form>
    </>
  );
}
