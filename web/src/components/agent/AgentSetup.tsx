// Chat's setup card (SPEC-0140): which coding agents this computer has, and
// the one chat uses. It shows where chat is used, in the right pane and on
// the Agent page.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";

import { getAgentSettings, saveAgentSettings } from "../../api/internalAgentClient";
import { queryKeys } from "../../api/queryKeys";
import type { AgentHarnessKind, AgentHarnessStatus, AgentSettingsResponse } from "../../api/types";
import { StatusMark } from "../StatusMark";

export const HARNESS_LABELS: Record<AgentHarnessKind, string> = {
  claude: "Claude Code",
  codex: "Codex",
};

const INSTALL_DOCS: Record<AgentHarnessKind, string> = {
  claude: "https://docs.anthropic.com/en/docs/claude-code/setup",
  codex: "https://github.com/openai/codex",
};

const KINDS: AgentHarnessKind[] = ["claude", "codex"];

type Readiness = "ready" | "signed-out" | "missing";

// The server runs a signed-out harness anyway (an API key reads as signed
// out), so only a missing one cannot be chosen.
function readiness(status: AgentHarnessStatus | undefined): Readiness {
  if (!status?.installed) return "missing";

  return status.loggedIn ? "ready" : "signed-out";
}

/** True on first use, when nothing is configured and more than one harness is ready. */
export function needsHarnessChoice(response: AgentSettingsResponse) {
  return (
    !response.settings.harness &&
    response.harnesses.filter((item) => readiness(item.status) === "ready").length > 1
  );
}

/**
 * The model chat runs: the session's, else the server's resolved model, else
 * the harness's default. Never the saved setting, which may name a model the
 * harness no longer offers.
 */
export function chatModelLabel(
  response: AgentSettingsResponse,
  harness: AgentHarnessKind | undefined,
  sessionModel?: string,
) {
  const models = response.harnesses.find((item) => item.kind === harness)?.status.models ?? [];
  const id = sessionModel || (harness === response.resolved.harness ? response.resolved.model : "");

  if (id) return models.find((option) => option.id === id)?.displayName || id;

  return (models.find((option) => option.default) ?? models[0])?.displayName;
}

export function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <>
      <code>{command}</code>{" "}
      <button
        type="button"
        className="button button--ghost"
        onClick={async () => {
          await navigator.clipboard?.writeText(command);
          setCopied(true);
        }}
      >
        {copied ? "Copied" : "Copy"}
      </button>
    </>
  );
}

function HarnessState({ kind, status }: { kind: AgentHarnessKind; status?: AgentHarnessStatus }) {
  switch (readiness(status)) {
    case "ready":
      return <StatusMark tone="success" label="Ready" />;
    case "signed-out":
      return status?.loginHint ? (
        <>
          <span>Signed out. Sign in by running this in Terminal:</span>
          <span className="agent-setup__command">
            <CopyCommand command={status.loginHint} />
          </span>
        </>
      ) : (
        <span>Signed out. Sign in to {HARNESS_LABELS[kind]} in Terminal.</span>
      );
    case "missing":
      return (
        <span>
          Not installed.{" "}
          <a href={INSTALL_DOCS[kind]} target="_blank" rel="noreferrer">
            How to install {HARNESS_LABELS[kind]}
          </a>
        </span>
      );
  }
}

export function AgentSetup({
  response,
  blocked,
}: {
  response: AgentSettingsResponse;
  /** True when no turn can run until a harness is installed or chosen. */
  blocked: boolean;
}) {
  const queryClient = useQueryClient();
  const titleID = useId();

  const store = (next: AgentSettingsResponse) =>
    queryClient.setQueryData(queryKeys.agent.settings(), next);

  const choose = useMutation({
    mutationFn: (kind: AgentHarnessKind) =>
      saveAgentSettings({ ...response.settings, harness: kind }),
    scope: { id: "agent-settings" },
    onSuccess: store,
  });

  const recheck = useMutation({
    mutationFn: () => getAgentSettings({ refresh: true }),
    onSuccess: store,
  });

  const failure = choose.error ?? recheck.error;

  return (
    <section className="agent-setup" aria-labelledby={titleID}>
      <h3 id={titleID}>{blocked ? "Set up chat" : "Choose a coding agent for chat"}</h3>
      <p className="agent-setup__lead">
        {blocked
          ? "Chat runs Claude Code or Codex on this computer. Install or sign in to one, then choose it."
          : "Claude Code and Codex are both ready. Chat uses the one you choose."}
      </p>
      <div className="agent-setup__rows" role="radiogroup" aria-labelledby={titleID}>
        {KINDS.map((kind) => {
          const status = response.harnesses.find((item) => item.kind === kind)?.status;
          // A choice reads as made from the click, not from the saved reply.
          const chosen = (choose.isPending ? choose.variables : response.settings.harness) === kind;
          const about = [status?.version, status?.account].filter(Boolean).join(" · ");

          return (
            <div key={kind} className={`agent-setup__row${chosen ? " is-chosen" : ""}`}>
              <label className="agent-setup__choice">
                <input
                  type="radio"
                  name={titleID}
                  value={kind}
                  checked={chosen}
                  disabled={readiness(status) === "missing" || choose.isPending}
                  onChange={() => choose.mutate(kind)}
                />
                <span className="agent-setup__name">{HARNESS_LABELS[kind]}</span>
              </label>
              {about && readiness(status) !== "missing" ? (
                <span className="agent-setup__about">{about}</span>
              ) : null}
              <div className="agent-setup__state">
                <HarnessState kind={kind} status={status} />
              </div>
              {status?.lastError ? (
                <details className="agent-setup__details">
                  <summary>Details</summary>
                  <code>{status.lastError}</code>
                </details>
              ) : null}
            </div>
          );
        })}
      </div>
      <div className="agent-setup__foot">
        <button
          type="button"
          className="button"
          disabled={recheck.isPending}
          onClick={() => recheck.mutate()}
        >
          {recheck.isPending ? "Checking…" : "Check again"}
        </button>
        {failure ? (
          <span className="agent-setup__error" role="alert">
            {failure instanceof Error ? failure.message : String(failure)}
          </span>
        ) : null}
      </div>
    </section>
  );
}
