import { useState } from "react";
import type { AgentHarnessKind, AgentHarnessStatus, AgentSession } from "../../api/types";

type Props = {
  session: AgentSession | null;
  harness: AgentHarnessKind | undefined;
  status: AgentHarnessStatus | undefined;
  resolutionReason: string;
  messageCount: number;
};

const harnessName = (kind?: AgentHarnessKind) =>
  kind === "claude" ? "Claude" : kind === "codex" ? "Codex" : "No harness";

const installHint = (kind?: AgentHarnessKind) =>
  kind === "claude" ? "Install Claude Code to use chat." : "Install the Codex CLI to use chat.";

/** True when no turn can be sent until the user installs or logs in to a harness. */
export function harnessBlocked(
  harness: AgentHarnessKind | undefined,
  status: AgentHarnessStatus | undefined,
) {
  return !harness || (status !== undefined && (!status.installed || !status.loggedIn));
}

export function AgentStatusHeader({
  session,
  harness,
  status,
  resolutionReason,
  messageCount,
}: Props) {
  let state = "Checking harness status…";

  if (session?.readOnly) {
    state = "Pre-harness history";
  } else if (status) {
    if (!status.installed) {
      state = installHint(harness);
    } else if (status.lastError) {
      const version =
        status.version && !status.lastError.includes(status.version) ? status.version : "";

      state = [status.lastError, version].filter(Boolean).join(" · ");
    } else if (!status.loggedIn) {
      state = "Login required";
    } else {
      state = ["Ready", status.version, status.account].filter(Boolean).join(" · ");
    }
  } else if (!harness) {
    state = resolutionReason;
  }

  return (
    <header className="view-header agent-thread__head">
      <div className="view-header__title-row">
        <h2>{session?.title || "Rhizome agent"}</h2>
        {session ? (
          <span className="view-header__count">
            {messageCount} {messageCount === 1 ? "message" : "messages"}
          </span>
        ) : null}
        <span className="view-header__about">
          <strong>{harnessName(session?.harness || harness)}</strong> · {state}
        </span>
      </div>
    </header>
  );
}

/**
 * Why chat is unavailable and what to do next, beside the composer. Renders
 * nothing while the harness is ready or its status is still loading.
 */
export function AgentAvailability({
  harness,
  status,
}: {
  harness: AgentHarnessKind | undefined;
  status: AgentHarnessStatus | undefined;
}) {
  const [copied, setCopied] = useState(false);

  if (!harness)
    return (
      <span className="agent-availability">
        No coding harness is available. Install Claude Code or the Codex CLI, then reload.
      </span>
    );

  if (!status) return null;

  if (!status.installed)
    return (
      <span className="agent-availability">
        {installHint(harness)}
        {status.lastError ? <span className="agent-muted"> {status.lastError}</span> : null}
      </span>
    );

  if (!status.loggedIn && !status.lastError && status.loginHint) {
    const hint = status.loginHint;

    return (
      <span className="agent-availability">
        Log in to {harnessName(harness)}: <code>{hint}</code>{" "}
        <button
          type="button"
          className="button button--ghost"
          onClick={async () => {
            await navigator.clipboard?.writeText(hint);
            setCopied(true);
          }}
        >
          {copied ? "Copied" : "Copy"}
        </button>
      </span>
    );
  }

  if (!status.loggedIn)
    return <span className="agent-availability">Log in to {harnessName(harness)} to chat.</span>;

  return null;
}
