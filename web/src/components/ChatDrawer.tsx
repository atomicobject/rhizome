// Chat, a tab of the app's one right pane (SPEC-0140). It talks to the same
// agent chat sessions as the Agent page, and each message carries what the
// user is looking at.
import { useQuery } from "@tanstack/react-query";
import { type CSSProperties, type FormEvent, useEffect, useRef, useState } from "react";

import {
  createAgentSession,
  getAgentSettings,
  interruptAgentSession,
  respondToAgentApproval,
} from "../api/internalAgentClient";
import { queryKeys } from "../api/queryKeys";
import type {
  AgentApprovalDecision,
  AgentHarnessKind,
  AgentPermissionMode,
  AgentViewContext,
} from "../api/types";
import { emptySettingsResponse } from "./agent/AgentSettingsPanel";
import { harnessBlocked } from "./agent/AgentStatusHeader";
import { useAgentThread } from "./agent/useAgentThread";
import { setChatDraft, setChatSession, startNewChat, useChatPane } from "./chat/chatSession";
import { ChatTimeline, ContextChip, Icon } from "./chat/ChatTimeline";
import { boundedContext, CONTEXT_KIND_LABELS, useViewContext } from "./chat/viewContext";
import { readSingleKey, typingIn } from "./KeyboardShortcuts";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import { PanelResizeHandle } from "./PanelResizeHandle";
import { PANE_RANGE, toggleChat, togglePaneOpen, useShellPanes } from "./shellPanes";
import { PanelIcon } from "./WorkbenchSidebar";

const IS_MAC = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

export const CHAT_SHORTCUT = IS_MAC ? "⌥⌘B" : "Ctrl+Alt+B";

/** ⌥⌘B (Ctrl+Alt+B elsewhere). `code`, because Option changes the typed key on a Mac. */
function isChatShortcut(event: KeyboardEvent): boolean {
  if (event.shiftKey || event.isComposing || event.code !== "KeyB" || !event.altKey) return false;

  return IS_MAC ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
}

const COMPOSER = "[data-right-pane] .chat-composer textarea";

/**
 * Runs a pane toggle. Closing the pane from inside it would drop focus to the
 * page, so focus moves to the top bar's pane toggle.
 */
function keepingFocus(toggle: () => void) {
  const fromInside = document.activeElement?.closest("[data-right-pane]");

  toggle();

  if (fromInside)
    requestAnimationFrame(() => {
      if (!document.activeElement?.closest("[data-right-pane]"))
        document.querySelector<HTMLElement>(".chat-toggle")?.focus();
    });
}

export const togglePane = () => keepingFocus(() => togglePaneOpen());

/** Shows the pane on Chat and focuses the composer, or hides Chat when it shows. */
export function toggleChatPane() {
  // Only the active section's pane renders a chat panel.
  const showing = Boolean(document.querySelector("[data-right-pane] .chat-panel"));

  keepingFocus(() => toggleChat(showing));

  if (!showing) requestAnimationFrame(() => document.querySelector<HTMLElement>(COMPOSER)?.focus());
}

function isPaneKey(event: KeyboardEvent): boolean {
  return (
    event.key === "]" &&
    !event.metaKey &&
    !event.ctrlKey &&
    !event.altKey &&
    !typingIn(event.target) &&
    readSingleKey()
  );
}

/**
 * The right pane's shortcuts, in every section: ] toggles the pane, and ⌥⌘B
 * shows it on Chat or hides it when Chat is showing.
 */
export function useChatShortcut() {
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      // The Agent page is the full chat and has no right pane.
      if (event.defaultPrevented || window.location.pathname.startsWith("/agent")) return;

      if (isChatShortcut(event)) {
        // Works from the composer too, so it can close the pane.
        event.preventDefault();

        if (!event.repeat) toggleChatPane();
      } else if (isPaneKey(event)) {
        event.preventDefault();

        if (!event.repeat) togglePane();
      }
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);
}

/** The top bar's right-pane toggle, at its right end: the pane's one toggle. */
export function PaneToggle({ disabled = false }: { disabled?: boolean }) {
  const { paneCollapsed } = useShellPanes();
  const label = paneCollapsed ? "Show right pane" : "Hide right pane";

  return (
    <button
      type="button"
      className="app-shell__sidebar-toggle chat-toggle"
      aria-label={label}
      aria-expanded={disabled ? undefined : !paneCollapsed}
      aria-keyshortcuts={IS_MAC ? "] Alt+Meta+B" : "] Control+Alt+B"}
      title={
        disabled ? "The Agent page is the full chat" : `${label} (]) · Chat (${CHAT_SHORTCUT})`
      }
      disabled={disabled}
      onClick={() => togglePaneOpen()}
    >
      <PanelIcon side="right" />
    </button>
  );
}

function startersFor(kind: AgentViewContext["kind"]): string[] {
  switch (kind) {
    case "note":
      return [
        "Summarize this note",
        "What links here, and why?",
        "Find notes that disagree with this",
      ];
    case "view":
      return ["Which rows need attention?", "Summarize what's here"];
    case "type":
    case "group":
      return ["Which records are missing fields?", "Summarize what's here"];
    case "ontology":
      return ["Explain how these types relate", "Which types have no records yet?"];
    default:
      return ["What changed this week?", "What should I look at first?"];
  }
}

const MODE_LABELS: Record<AgentPermissionMode, { label: string; title: string }> = {
  auto: {
    label: "Auto",
    title:
      "Claude runs in auto mode and Codex in Approve for me: a reviewer allows routine actions and asks you only about risky ones.",
  },
  "approval-required": { label: "Ask first", title: "The agent asks before every action." },
  "auto-accept-edits": {
    label: "Auto edits",
    title: "File edits run without asking; other actions ask first.",
  },
  "full-access": { label: "Full access", title: "Every action runs without asking." },
};

const HARNESS_LABELS: Record<AgentHarnessKind, string> = { claude: "Claude", codex: "Codex" };

function openInAgent(sessionID: string) {
  window.history.pushState({}, "", `/agent?session=${encodeURIComponent(sessionID)}`);
  notifyLocationChange();
}

const errorText = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause));

/** New chat and Open in Agent, in the pane's header while Chat shows. */
export function ChatHeaderActions() {
  const { sessionID } = useChatPane();

  if (!sessionID) return null;

  return (
    <div className="chat-pane-actions">
      <button type="button" title="New chat" aria-label="New chat" onClick={startNewChat}>
        <Icon d="M8 3.5v9M3.5 8h9" />
      </button>
      <button
        type="button"
        title="Open in Agent"
        aria-label="Open in Agent"
        onClick={() => openInAgent(sessionID)}
      >
        <Icon d="M6.5 3.5H4a.5.5 0 0 0-.5.5v8a.5.5 0 0 0 .5.5h8a.5.5 0 0 0 .5-.5V9.5M9 3.5h3.5V7M12.5 3.5 7.5 8.5" />
      </button>
    </div>
  );
}

/** The chat tab's contents: the conversation and the composer. */
export function ChatPanel() {
  const log = useRef<HTMLDivElement>(null);
  const { pathname, search } = useLocationSnapshot();
  const context = useViewContext();
  const { sessionID, draft } = useChatPane();
  const [error, setError] = useState("");
  // The page whose context the user detached; moving on re-attaches.
  const [detachedFrom, setDetachedFrom] = useState<string | null>(null);
  const routeKey = `${pathname}?${search}`;
  const attachContext = detachedFrom !== routeKey;
  const contextRef = useRef(context);

  contextRef.current = context;

  const settingsQuery = useQuery({
    queryKey: queryKeys.agent.settings(),
    queryFn: ({ signal }) => getAgentSettings({ signal }),
    // A harness that was busy or slow to answer its probe may answer later.
    refetchInterval: (query) => (query.state.data?.resolved.harness ? false : 15_000),
  });

  const thread = useAgentThread(sessionID, () => ({
    title: contextRef.current.title,
    route: contextRef.current.route,
  }));

  const session = thread.sessionQuery.data?.session;
  const settings = settingsQuery.data ?? emptySettingsResponse;
  const harness = session?.harness || settings.settings.harness || settings.resolved.harness;
  const status = settings.harnesses.find((item) => item.kind === harness)?.status;
  const blocked = settingsQuery.isSuccess && harnessBlocked(harness, status);
  const mode = harness ? settings.settings.harnesses[harness].permissionMode : "auto";
  const modelID = session?.model || (harness ? settings.settings.harnesses[harness].model : "");
  const models = status?.models ?? [];

  const model = modelID
    ? models.find((option) => option.id === modelID)?.displayName || modelID
    : (models.find((option) => option.default) ?? models[0])?.displayName;

  // Held from the first keypress until the send returns, so a second Enter,
  // even in the same frame, cannot start a second session or turn.
  const sendingRef = useRef(false);
  const [starting, setStarting] = useState(false);
  const running = starting || thread.isSending || thread.turnRunning;
  const empty = thread.messages.length === 0 && thread.events.length === 0;

  // A session deleted on the Agent page, which archives it, starts over.
  useEffect(() => {
    if (sessionID && (thread.sessionQuery.isError || session?.archived)) startNewChat();
  }, [thread.sessionQuery.isError, session?.archived, sessionID]);

  // The log stays on the newest message, also when the pane resizes or the
  // page settles around it, unless the user scrolled up to read.
  const pinned = useRef(true);

  useEffect(() => {
    if (log.current) log.current.scrollTop = log.current.scrollHeight;
    pinned.current = true;
  }, [thread.messages, thread.events]);

  useEffect(() => {
    const element = log.current;

    if (!element || !("ResizeObserver" in window)) return;

    const observer = new ResizeObserver(() => {
      if (pinned.current) element.scrollTop = element.scrollHeight;
    });

    observer.observe(element);

    return () => observer.disconnect();
  }, []);

  const send = async (text: string) => {
    const message = text.trim();

    if (!message || running || blocked || sendingRef.current) return;
    sendingRef.current = true;
    setStarting(true);
    setError("");
    const sent = attachContext ? boundedContext(context) : undefined;

    try {
      let target = sessionID;

      if (!target) {
        const created = await createAgentSession(message.slice(0, 48));
        target = created.session.id;
        setChatSession(target);
      }

      setChatDraft("");
      await thread.send(target, message, sent);
    } catch (cause) {
      setError(errorText(cause));
    } finally {
      sendingRef.current = false;
      setStarting(false);
    }
  };

  const stop = async () => {
    if (!sessionID) return;

    try {
      await interruptAgentSession(sessionID);
    } catch (cause) {
      setError(errorText(cause));
    }
  };

  const approve = async (requestID: string, decision: AgentApprovalDecision) => {
    try {
      await respondToAgentApproval(sessionID, requestID, decision);
    } catch (cause) {
      setError(errorText(cause));
      throw cause;
    }
  };

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    void send(draft);
  };

  return (
    <div className="chat-panel">
      <div
        className="chat-drawer__log"
        ref={log}
        role="log"
        aria-live="polite"
        aria-label="Conversation"
        onScroll={(event) => {
          const element = event.currentTarget;

          pinned.current = element.scrollHeight - element.scrollTop - element.clientHeight < 24;
        }}
      >
        {blocked ? (
          <div className="chat-drawer__empty" role="status">
            <p>
              Chat needs Codex or Claude Code.{" "}
              <a
                href="/agent"
                onClick={(event) => {
                  event.preventDefault();
                  window.history.pushState({}, "", "/agent");
                  notifyLocationChange();
                }}
              >
                Set one up on the Agent page
              </a>
              .
            </p>
          </div>
        ) : empty && !(sessionID && thread.sessionQuery.isPending) ? (
          <div className="chat-drawer__empty">
            <p>Ask about what you're looking at.</p>
            <div className="chat-starters">
              {startersFor(context.kind).map((starter) => (
                <button key={starter} type="button" onClick={() => void send(starter)}>
                  {starter}
                </button>
              ))}
            </div>
          </div>
        ) : null}
        <ChatTimeline
          messages={thread.messages}
          events={thread.events}
          running={running}
          autoNote={mode === "auto" ? "Allowed by auto mode" : undefined}
          onApproval={approve}
        />
      </div>
      {error ? (
        <p className="chat-error chat-error--composer" role="alert">
          {error}
        </p>
      ) : null}
      <form className="agent-composer chat-composer" onSubmit={onSubmit}>
        <div className="agent-composer__frame">
          <div className="chat-composer__context">
            {attachContext ? (
              <ContextChip context={context} onRemove={() => setDetachedFrom(routeKey)} />
            ) : (
              <button type="button" className="chat-attach" onClick={() => setDetachedFrom(null)}>
                + {CONTEXT_KIND_LABELS[context.kind]} “{context.title}”
              </button>
            )}
          </div>
          <textarea
            value={draft}
            aria-label="Message"
            placeholder={`Ask about ${context.title}…`}
            rows={2}
            disabled={blocked}
            onChange={(event) => setChatDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault();
                void send(draft);
              }
            }}
          />
          <div className="agent-composer__bar">
            <span className="chat-composer__agent" title={MODE_LABELS[mode].title}>
              {harness ? HARNESS_LABELS[harness] : "No agent"}
              {model ? ` · ${model}` : ""}{" "}
              <span className="chat-composer__mode">{MODE_LABELS[mode].label}</span>
            </span>
            {running ? (
              <button type="button" className="button" onClick={() => void stop()}>
                Stop
              </button>
            ) : (
              <button
                type="submit"
                className={`button${draft.trim() ? " button--primary" : ""}`}
                disabled={!draft.trim() || blocked}
              >
                Send
              </button>
            )}
          </div>
        </div>
      </form>
    </div>
  );
}

type DrawerStyle = CSSProperties & { "--pane-width": string };

/**
 * The right pane for sections with nothing of their own to show there, so it
 * holds only Chat. Notes hosts Chat as a tab of its own pane instead.
 */
export function ChatDrawer() {
  const ref = useRef<HTMLElement>(null);
  const panes = useShellPanes();

  if (panes.paneCollapsed) return null;

  const style: DrawerStyle = { "--pane-width": `${panes.paneWidth}px` };

  return (
    <aside
      ref={ref}
      className={`chat-drawer${panes.paneOverlay ? " is-overlay" : ""}`}
      style={style}
      aria-label="Chat"
      data-right-pane
      onKeyDown={(event) => {
        // An overlay covers the content, so Escape gives the content back.
        if (panes.paneOverlay && event.key === "Escape" && !event.defaultPrevented) {
          event.preventDefault();
          togglePane();
        }
      }}
    >
      {panes.paneOverlay ? null : (
        <PanelResizeHandle
          label="Resize right pane"
          range={PANE_RANGE}
          value={panes.paneWidth}
          edge="start"
          cssVar="--pane-width"
          target={ref}
          onResize={panes.setPaneWidth}
        />
      )}
      {/* Same header as the Notes pane, with Chat its only tab. */}
      <div className="notes-right-rail__header">
        <div className="notes-right-rail__tabs" role="tablist" aria-label="Right pane">
          <button
            type="button"
            role="tab"
            id="chat-drawer-tab"
            className="notes-right-rail__tab"
            aria-selected
            aria-controls="chat-drawer-panel"
          >
            Chat
          </button>
        </div>
        <ChatHeaderActions />
        <button
          type="button"
          className="notes-right-rail__close"
          aria-label="Hide right pane"
          aria-keyshortcuts="]"
          title="Hide right pane (])"
          onClick={togglePane}
        >
          <Icon d="M4 4l8 8M12 4l-8 8" />
        </button>
      </div>
      <div
        className="notes-right-rail__chat"
        id="chat-drawer-panel"
        role="tabpanel"
        aria-labelledby="chat-drawer-tab"
      >
        <ChatPanel />
      </div>
    </aside>
  );
}
