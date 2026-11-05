import { StatusMark } from "../StatusMark";
import { useState } from "react";
import { DisclosureChevron } from "../DisclosureChevron";
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

/** True when no turn can be sent until the user picks or installs a harness. */
export function harnessBlocked(
  harness: AgentHarnessKind | undefined,
  status: AgentHarnessStatus | undefined,
) {
  // The server runs a configured harness without checking login (an API key
  // or a timed-out probe reads as logged out), so login is a hint, not a block.
  return !harness || (status !== undefined && !status.installed);
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
      state = status.lastError;
    } else if (!status.loggedIn) {
      state = "Login required";
    } else {
      state = "Ready";
    }
  } else if (!harness) {
    state = resolutionReason;
  }

  // The version and account are diagnostics: behind a disclosure, and the
  // version only when the state line does not already name it.
  const version =
    status?.version && !(status.lastError ?? "").includes(status.version) ? status.version : "";

  const details = session?.readOnly ? [] : [version, status?.account].filter(Boolean);

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
          {harnessName(session?.harness || harness)} ·{" "}
          {state === "Ready" ? <StatusMark tone="success" label={state} /> : state}
        </span>
        {details.length ? (
          <div className="view-header__actions">
            <details className="header-details">
              <summary className="button button--ghost">
                Details <DisclosureChevron />
              </summary>
              <dl>
                {version ? (
                  <>
                    <dt>Version</dt>
                    <dd>
                      <code>{version}</code>
                    </dd>
                  </>
                ) : null}
                {status?.account ? (
                  <>
                    <dt>Account</dt>
                    <dd>{status.account}</dd>
                  </>
                ) : null}
              </dl>
            </details>
          </div>
        ) : null}
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
