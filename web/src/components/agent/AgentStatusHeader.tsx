import { useState } from "react";
import type { AgentHarnessKind, AgentHarnessStatus, AgentSession } from "../../api/types";

type Props = {
  session: AgentSession | null;
  harness: AgentHarnessKind | undefined;
  status: AgentHarnessStatus | undefined;
  resolutionReason: string;
  messageCount: number;
  drawerOpen: boolean;
  onToggleDrawer: () => void;
};

const harnessName = (kind?: AgentHarnessKind) =>
  kind === "claude" ? "Claude" : kind === "codex" ? "Codex" : "No harness";

const installHint = (kind?: AgentHarnessKind) =>
  kind === "claude" ? "Install Claude Code to use chat." : "Install the Codex CLI to use chat.";

export function AgentStatusHeader({
  session,
  harness,
  status,
  resolutionReason,
  messageCount,
  drawerOpen,
  onToggleDrawer,
}: Props) {
  const [copied, setCopied] = useState(false);

  const copyLoginHint = async () => {
    if (!status?.loginHint) return;
    await navigator.clipboard?.writeText(status.loginHint);
    setCopied(true);
  };

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
    <header className="agent-thread__head">
      <div className="agent-thread__title">
        <button
          type="button"
          className="agent-drawer-toggle"
          aria-controls="agent-sidebar"
          aria-expanded={drawerOpen}
          aria-label={drawerOpen ? "Close agent sidebar" : "Open agent sidebar"}
          onClick={onToggleDrawer}
        >
          <span />
          <span />
          <span />
        </button>
        <div>
          <h2>{session?.title || "Rhizome agent"}</h2>
          <p>
            <strong>{harnessName(session?.harness || harness)}</strong> · {state}
          </p>
          {!session?.readOnly && status && !status.installed && status.lastError ? (
            <p className="agent-muted">{status.lastError}</p>
          ) : null}
          {!session?.readOnly &&
          status?.installed &&
          !status.loggedIn &&
          !status.lastError &&
          status.loginHint ? (
            <p>
              <code>{status.loginHint}</code>{" "}
              <button type="button" onClick={copyLoginHint}>
                {copied ? "Copied" : "Copy"}
              </button>
            </p>
          ) : null}
        </div>
      </div>
      <span className="agent-pill">{messageCount} messages</span>
    </header>
  );
}
