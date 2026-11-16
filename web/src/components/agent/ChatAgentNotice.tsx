// Why a message cannot send (SPEC-0140): the notice above the composer when
// the chosen agent is signed out or not installed, or the line that says the
// next message starts a new chat with another agent.
import { useState } from "react";

import type { AgentHarnessKind } from "../../api/types";
import { StatusIcon } from "../StatusMark";
import { type Blocker, choiceLabel, HARNESS_LABELS, INSTALL_DOCS } from "./chatAgent";
import type { ChatAgent } from "./ChatAgentControls";

function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <span className="chat-notice__command">
      <code>{command}</code>
      <button
        type="button"
        aria-label={copied ? "Copied" : `Copy ${command}`}
        title={copied ? "Copied" : "Copy"}
        onClick={async () => {
          await navigator.clipboard?.writeText(command);
          setCopied(true);
        }}
      >
        <svg aria-hidden="true" viewBox="0 0 12 12">
          {copied ? (
            <path d="m2.5 6.5 2.5 2.5 4.5-5" fill="none" stroke="currentColor" strokeWidth="1.3" />
          ) : (
            <>
              <rect
                x="3.5"
                y="3.5"
                width="7"
                height="7"
                rx="1.2"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.2"
              />
              <path
                d="M8.5 2.5V2A1 1 0 0 0 7.5 1H2A1 1 0 0 0 1 2v5.5a1 1 0 0 0 1 1h.5"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.2"
              />
            </>
          )}
        </svg>
      </button>
    </span>
  );
}

function InstallLink({ harness, children }: { harness: AgentHarnessKind; children: string }) {
  return (
    <a href={INSTALL_DOCS[harness]} target="_blank" rel="noreferrer">
      {children}
    </a>
  );
}

function NoticeText({ blocker }: { blocker: Blocker }) {
  switch (blocker.kind) {
    case "signed-out":
      return blocker.command ? (
        <>
          {HARNESS_LABELS[blocker.harness]} is signed out. Run{" "}
          <CopyCommand command={blocker.command} /> in Terminal.
        </>
      ) : (
        <>{HARNESS_LABELS[blocker.harness]} is signed out. Sign in to it in Terminal.</>
      );
    case "not-installed":
      return (
        <>
          {HARNESS_LABELS[blocker.harness]} isn't installed.{" "}
          <InstallLink harness={blocker.harness}>How to install</InstallLink>
        </>
      );
    case "none-installed":
      return (
        <>
          No coding agent is installed. Install{" "}
          <InstallLink harness="claude">Claude Code</InstallLink> or{" "}
          <InstallLink harness="codex">Codex</InstallLink>.
        </>
      );
    case "all-off":
      return <>Every coding agent is turned off in Settings → Chat.</>;
  }
}

/**
 * Above the composer: why a message cannot send and what to do, or that the
 * next message starts a new chat with another agent.
 */
export function ChatAgentNotice({ agent }: { agent: ChatAgent }) {
  const { blocker, choice, fallback } = agent;

  const failure = agent.error ? (
    <p className="chat-agent-hint chat-agent-hint--error" role="alert">
      {agent.error instanceof Error ? agent.error.message : String(agent.error)}
    </p>
  ) : null;

  if (!blocker)
    return (
      <>
        {failure}
        {agent.newChat && choice ? (
          <p className="chat-agent-hint">
            Sends in a new chat with {HARNESS_LABELS[choice.harness]}
          </p>
        ) : null}
      </>
    );

  return (
    <div className="chat-notice" role="status">
      <div className="chat-notice__row">
        <StatusIcon tone={blocker.kind === "signed-out" ? "neutral" : "muted"} />
        <p>
          <NoticeText blocker={blocker} />
        </p>
      </div>
      <div className="chat-notice__actions">
        <button type="button" className="button" disabled={agent.checking} onClick={agent.recheck}>
          {agent.checking ? "Checking…" : "Check again"}
        </button>
        {fallback ? (
          <button
            type="button"
            className="chat-notice__fallback"
            onClick={() => agent.choose(fallback)}
          >
            Use {choiceLabel(agent.response, fallback).model} instead
          </button>
        ) : null}
      </div>
      {failure}
    </div>
  );
}
