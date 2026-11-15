// Chat, a tab of the app's one right pane (SPEC-0140). It talks to the same
// agent chat sessions as the Agent page, and each message carries what the
// user is looking at.
import { type CSSProperties, type FormEvent, useEffect, useRef, useState } from "react";

import {
  createAgentSession,
  interruptAgentSession,
  respondToAgentApproval,
} from "../api/internalAgentClient";
import { ApiError } from "../api/client";
import type { AgentApprovalDecision, AgentViewContext } from "../api/types";
import { ChatAgentControls, useChatAgent } from "./agent/ChatAgentControls";
import { ChatAgentNotice } from "./agent/ChatAgentNotice";
import { useAgentThread } from "./agent/useAgentThread";
import {
  beginChatSend,
  cancelChatSend,
  chatSendLive,
  clearChatDraftIf,
  endChatSend,
  postChatSend,
  setChatDraft,
  setChatSession,
  startNewChat,
  useChatPane,
} from "./chat/chatSession";
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
  const { sessionID, draft, sending } = useChatPane();
  const [error, setError] = useState("");
  // The page whose context the user detached; moving on re-attaches.
  const [detachedFrom, setDetachedFrom] = useState<string | null>(null);
  const routeKey = `${pathname}?${search}`;
  const attachContext = detachedFrom !== routeKey;
  const contextRef = useRef(context);

  contextRef.current = context;

  const thread = useAgentThread(sessionID, () => ({
    title: contextRef.current.title,
    route: contextRef.current.route,
  }));

  const session = thread.sessionQuery.data?.session;
  const agent = useChatAgent(session);
  const blocked = Boolean(agent.blocker);

  // The store holds a send from the first keypress until it returns, so a
  // second Enter, even from a pane mounted meanwhile, cannot start a second
  // session or turn.
  const running = sending || thread.isSending || thread.turnRunning;
  const empty = thread.messages.length === 0 && thread.events.length === 0;

  // A session deleted on the Agent page, which archives it, starts over.
  useEffect(() => {
    // A failed read for any other reason keeps the conversation and draft.
    const missing =
      thread.sessionQuery.error instanceof ApiError && thread.sessionQuery.error.status === 404;

    if (sessionID && (missing || session?.archived)) startNewChat();
  }, [thread.sessionQuery.error, session?.archived, sessionID]);

  // The log stays on the newest message, also when the pane resizes or the
  // page settles around it, unless the user scrolled up to read.
  const pinned = useRef(true);

  // New messages and events follow the log only while it is pinned to the
  // bottom; sending pins it again.
  useEffect(() => {
    if (log.current && pinned.current) log.current.scrollTop = log.current.scrollHeight;
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

    if (!message || running || blocked) return;

    const id = beginChatSend();

    if (!id) return;
    pinned.current = true;
    setError("");
    const sent = attachContext ? boundedContext(context) : undefined;

    try {
      if (!(await agent.saved()) || !chatSendLive(id)) return;

      // A choice of another agent starts a new chat with it.
      let target = agent.startsNewChat() ? "" : sessionID;

      if (!target) {
        const created = await createAgentSession(message.slice(0, 48));
        target = created.session.id;
        setChatSession(target);
      }

      if (!postChatSend(id)) return;
      clearChatDraftIf(text);
      await thread.send(target, message, sent);
    } catch (cause) {
      setError(errorText(cause));
    } finally {
      endChatSend(id);
    }
  };

  const stop = async () => {
    // A message still waiting for its choice to save or its chat to open
    // has no turn to interrupt.
    if (cancelChatSend() || !sessionID) return;

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
        {empty && !blocked && !(sessionID && thread.sessionQuery.isPending) ? (
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
          autoNote={agent.choice?.permissionMode === "auto" ? "Allowed by auto mode" : undefined}
          onApproval={approve}
        />
      </div>
      {error ? (
        <p className="chat-error chat-error--composer" role="alert">
          {error}
        </p>
      ) : null}
      <form className="agent-composer chat-composer" onSubmit={onSubmit}>
        <ChatAgentNotice agent={agent} />
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
            onChange={(event) => setChatDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault();
                void send(draft);
              }
            }}
          />
          <div className="agent-composer__bar">
            <ChatAgentControls agent={agent} />
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

  if (!panes.chatOpen) return null;

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
