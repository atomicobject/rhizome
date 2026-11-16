import { StatusMark } from "../StatusMark";
import { DisclosureChevron } from "../DisclosureChevron";
import type { AgentHarnessKind, AgentHarnessStatus, AgentSession } from "../../api/types";
import { CopyCommand, HARNESS_LABELS } from "./AgentSetup";

type Props = {
  session: AgentSession | null;
  harness: AgentHarnessKind | undefined;
  status: AgentHarnessStatus | undefined;
  messageCount: number;
  model: string | undefined;
};

const harnessName = (kind: AgentHarnessKind) => HARNESS_LABELS[kind];

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

export function AgentStatusHeader({ session, harness, status, messageCount, model }: Props) {
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
  }

  const shown = session?.harness || harness;

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
          {shown ? (
            <>
              {harnessName(shown)} · {model ? `${model} · ` : ""}
              {state === "Ready" ? <StatusMark tone="success" label={state} /> : state}
            </>
          ) : session?.readOnly ? (
            state
          ) : (
            "Chat not set up"
          )}
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
 * How to sign in to an installed harness, beside the composer. A missing or
 * unchosen harness has the setup card instead, so this renders nothing then.
 */
export function AgentAvailability({
  harness,
  status,
}: {
  harness: AgentHarnessKind | undefined;
  status: AgentHarnessStatus | undefined;
}) {
  if (!harness || !status?.installed || status.loggedIn) return null;

  return (
    <span className="agent-availability">
      {status.loginHint && !status.lastError ? (
        <>
          Sign in to {harnessName(harness)}: <CopyCommand command={status.loginHint} />
        </>
      ) : (
        `Sign in to ${harnessName(harness)} to chat.`
      )}
    </span>
  );
}
