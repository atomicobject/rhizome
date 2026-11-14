// PROTOTYPE: chat as a tab of the app's one right pane. The conversation is
// mocked and nothing reaches a harness; it exists to judge placement,
// toggling, resizing, and how "what you're looking at" reads.
import {
  type CSSProperties,
  type FormEvent,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";

import { readSingleKey, typingIn } from "./KeyboardShortcuts";
import { useLocationSnapshot } from "./locationStore";
import { parseNotesLocation } from "./notesRoute";
import { isOntologyPath, parseOntologyRoute } from "./ontologyRoute";
import { PanelResizeHandle } from "./PanelResizeHandle";
import { PANE_RANGE, toggleChat, togglePaneOpen, useShellPanes } from "./shellPanes";
import { useViewCatalogQuery } from "./useNotesQueries";
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
function toggleChatPane() {
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
      if (event.defaultPrevented) return;

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
export function PaneToggle() {
  const { paneCollapsed } = useShellPanes();
  const label = paneCollapsed ? "Show right pane" : "Hide right pane";

  return (
    <button
      type="button"
      className="app-shell__sidebar-toggle chat-toggle"
      aria-label={label}
      aria-expanded={!paneCollapsed}
      aria-keyshortcuts={IS_MAC ? "] Alt+Meta+B" : "] Control+Alt+B"}
      title={`${label} (]) · Chat (${CHAT_SHORTCUT})`}
      onClick={() => togglePaneOpen()}
    >
      <PanelIcon side="right" />
    </button>
  );
}

type ViewContext = { kind: string; label: string; detail?: string };

function sameContext(a: ViewContext | null, b: ViewContext | null) {
  return a?.kind === b?.kind && a?.label === b?.label;
}

function describeLocation(pathname: string, search: string, hash: string): ViewContext {
  if (isOntologyPath(pathname)) {
    const selection = parseOntologyRoute(pathname);

    return selection.kind === "type"
      ? { kind: "Type", label: selection.name, detail: "Ontology · schema, fields, relations" }
      : { kind: "Ontology", label: "Atlas", detail: "All types and how they relate" };
  }

  const section = pathname.split("/")[1] || "notes";

  if (section !== "notes") {
    return { kind: "Section", label: section[0].toUpperCase() + section.slice(1) };
  }

  const location = parseNotesLocation(pathname, search, hash);

  if (location.note) {
    const name = location.note.split("/").pop() ?? location.note;

    return {
      kind: "Note",
      label: name.replace(/\.md$/, ""),
      detail: location.fragment ? `${location.note} › ${location.fragment}` : location.note,
    };
  }

  if (location.view)
    return { kind: "View", label: location.view, detail: "Filters, sort, visible rows" };

  const selection = location.selection;

  switch (selection.kind) {
    case "type":
      return { kind: "Type", label: selection.slug, detail: "Visible rows, filters, counts" };
    case "group":
      return { kind: "Group", label: selection.group, detail: "Visible rows, filters, counts" };
    case "issues":
      return { kind: "Notes", label: "Problems" };
    case "modified":
      return { kind: "Notes", label: "Changes" };
    default:
      return { kind: "Notes", label: "All notes" };
  }
}

type ActivityIcon = "edit" | "read" | "screenshot" | "denied";

type Message =
  | { id: number; role: "user"; text: string; context: ViewContext | null }
  | { id: number; role: "assistant"; text: string }
  /** Something the agent did or read on its own; `note` says who allowed it. */
  | { id: number; role: "activity"; icon: ActivityIcon; text: string; note?: string }
  | {
      id: number;
      role: "approval";
      title: string;
      detail: string;
      state: "pending" | "allowed" | "denied";
    };

const ACTION_ITEMS: ViewContext = {
  kind: "View",
  label: "Action items",
  detail: "status ≠ done · sorted by due",
};

const SEED: Message[] = [
  { id: 1, role: "user", text: "Which of these are overdue?", context: ACTION_ITEMS },
  { id: 2, role: "activity", icon: "read", text: "Ran the view's query for all 41 rows" },
  {
    id: 3,
    role: "assistant",
    text: "3 are past due: “Renew TLS cert” (Nov 2), “Send Q4 plan to Rachel” (Nov 8), and “Close indexer effort” (Nov 11).",
  },
  { id: 4, role: "user", text: "Mark the indexer one done.", context: ACTION_ITEMS },
  {
    id: 5,
    role: "activity",
    icon: "edit",
    text: "Checked “Close indexer effort” in Indexer reliability",
    note: "Allowed by auto mode · docs/efforts/indexer-reliability.md",
  },
  { id: 6, role: "assistant", text: "Done. The view now shows 2 overdue items." },
  {
    id: 7,
    role: "user",
    text: "Why does the table on this note look empty?",
    context: { kind: "Note", label: "Weekly sync", detail: "notes/meetings/weekly-sync.md" },
  },
  { id: 8, role: "activity", icon: "screenshot", text: "Took a screenshot of the page" },
  {
    id: 9,
    role: "assistant",
    text: "Its embedded view filters to owner = you, and all three items on this note belong to Priya. Clear the Owner filter to see them.",
  },
  {
    id: 10,
    role: "user",
    text: "Delete the action items that have been done for more than a year.",
    context: ACTION_ITEMS,
  },
  {
    id: 11,
    role: "approval",
    title: "Delete 14 action items across 9 notes",
    detail: "Auto mode asks before deleting content. Each note keeps its history in Git.",
    state: "pending",
  },
];

// The conversation, shared by the pane's header and its panel, and kept when
// the pane moves between sections.
type ChatState = { messages: Message[]; draft: string; working: boolean };

let chat: ChatState = { messages: SEED, draft: "", working: false };

const chatListeners = new Set<() => void>();

function setChat(change: Partial<ChatState>) {
  chat = { ...chat, ...change };

  for (const listener of chatListeners) listener();
}

function subscribeChat(listener: () => void) {
  chatListeners.add(listener);

  return () => chatListeners.delete(listener);
}

const useChat = () => useSyncExternalStore(subscribeChat, () => chat);

// The Notes tab strip's title for the shown note or view, which the URL
// cannot give: it names a file or a view id.
let notesTitle: string | null = null;

const titleListeners = new Set<() => void>();

export function publishNotesTitle(title: string | null) {
  if (title === notesTitle) return;
  notesTitle = title;

  for (const listener of titleListeners) listener();
}

function subscribeTitle(listener: () => void) {
  titleListeners.add(listener);

  return () => titleListeners.delete(listener);
}

function useViewContext(): ViewContext {
  const { pathname, search, hash } = useLocationSnapshot();
  const located = describeLocation(pathname, search, hash);
  const title = useSyncExternalStore(subscribeTitle, () => notesTitle);
  const titled = located.kind === "Note" || located.kind === "View";
  const catalog = useViewCatalogQuery(located.kind === "View" && !title);

  if (titled && title) return { ...located, label: title };

  const viewName = catalog.data?.views.find((view) => view.id === located.label)?.name;

  return viewName ? { ...located, label: viewName } : located;
}

function ContextChip({ context, onRemove }: { context: ViewContext; onRemove?: () => void }) {
  return (
    <span className="chat-context" title={context.detail}>
      <span className="chat-context__kind">{context.kind}</span>
      <span className="chat-context__label">{context.label}</span>
      {onRemove ? (
        <button type="button" aria-label="Don't attach what I'm looking at" onClick={onRemove}>
          ×
        </button>
      ) : null}
    </span>
  );
}

function Icon({ d, strokeWidth = 1.4 }: { d: string; strokeWidth?: number }) {
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

const ACTIVITY_ICONS: Record<ActivityIcon, string> = {
  edit: "m3.5 8.5 3 3 6-7",
  read: "M3 3.5h10M3 6.5h10M3 9.5h6M3 12.5h4",
  screenshot:
    "M2.5 5.5v-2h2M11.5 3.5h2v2M13.5 10.5v2h-2M4.5 12.5h-2v-2M6 8a2 2 0 1 0 4 0 2 2 0 0 0-4 0",
  denied: "M4.5 4.5l7 7M11.5 4.5l-7 7",
};

function Activity({ icon, text, note }: { icon: ActivityIcon; text: string; note?: string }) {
  return (
    <div className={`chat-activity chat-activity--${icon}`} title={note}>
      <Icon d={ACTIVITY_ICONS[icon]} strokeWidth={icon === "edit" ? 1.6 : 1.4} />
      <span>{text}</span>
    </div>
  );
}

function startersFor(kind: string): string[] {
  switch (kind) {
    case "Note":
      return [
        "Summarize this note",
        "What links here, and why?",
        "Find notes that disagree with this",
      ];
    case "View":
      return ["Which rows need attention?", "Summarize what's here", "Group these by owner"];
    case "Type":
    case "Group":
      return ["Which records are missing fields?", "Summarize what's here"];
    case "Ontology":
      return ["Explain how these types relate", "Which types have no records yet?"];
    default:
      return ["What changed this week?", "What should I look at first?"];
  }
}

/** New chat and Open in Agent, in the pane's header while Chat shows. */
export function ChatHeaderActions() {
  const { messages } = useChat();

  return (
    <div className="chat-pane-actions">
      {messages.length ? (
        <button
          type="button"
          title="New chat"
          aria-label="New chat"
          onClick={() => setChat({ messages: [], draft: "", working: false })}
        >
          <Icon d="M8 3.5v9M3.5 8h9" />
        </button>
      ) : null}
      <button type="button" title="Open in Agent" aria-label="Open in Agent">
        <Icon d="M6.5 3.5H4a.5.5 0 0 0-.5.5v8a.5.5 0 0 0 .5.5h8a.5.5 0 0 0 .5-.5V9.5M9 3.5h3.5V7M12.5 3.5 7.5 8.5" />
      </button>
    </div>
  );
}

let replyTimer = 0;

/** The chat tab's contents: the conversation and the composer. */
export function ChatPanel() {
  const log = useRef<HTMLDivElement>(null);
  const { pathname, search } = useLocationSnapshot();
  const context = useViewContext();
  const { messages, draft, working } = useChat();
  // The page whose context the user detached; moving on re-attaches.
  const [detachedFrom, setDetachedFrom] = useState<string | null>(null);
  const routeKey = `${pathname}?${search}`;
  const attachContext = detachedFrom !== routeKey;

  // The log stays on the newest message, also when the pane resizes or the
  // page settles around it, unless the user scrolled up to read.
  const pinned = useRef(true);

  useEffect(() => {
    if (log.current) log.current.scrollTop = log.current.scrollHeight;
    pinned.current = true;
  }, [messages, working]);

  useEffect(() => {
    const element = log.current;

    if (!element || !("ResizeObserver" in window)) return;

    const observer = new ResizeObserver(() => {
      if (pinned.current) element.scrollTop = element.scrollHeight;
    });

    observer.observe(element);

    return () => observer.disconnect();
  }, []);

  const send = (text: string) => {
    const message = text.trim();

    if (!message || working) return;
    const sent = attachContext ? context : null;

    setChat({
      messages: [...chat.messages, { id: Date.now(), role: "user", text: message, context: sent }],
      draft: "",
      working: true,
    });
    window.clearTimeout(replyTimer);
    replyTimer = window.setTimeout(
      () =>
        setChat({
          working: false,
          messages: [
            ...chat.messages,
            {
              id: Date.now(),
              role: "assistant",
              text: `Prototype reply. A real agent would get ${
                sent ? `${sent.kind.toLowerCase()} “${sent.label}”` : "no page context"
              }, and would read the page text or take a screenshot only if it needed them.`,
            },
          ],
        }),
      1400,
    );
  };

  const stop = () => {
    window.clearTimeout(replyTimer);
    setChat({ working: false });
  };

  const decide = (id: number, state: "allowed" | "denied") =>
    setChat({
      messages: chat.messages.map((message) =>
        message.id === id && message.role === "approval" ? { ...message, state } : message,
      ),
    });

  const onSubmit = (event: FormEvent) => {
    event.preventDefault();
    send(draft);
  };

  let previousContext: ViewContext | null = null;

  return (
    <div className="chat-panel">
      <div
        className="chat-drawer__log"
        ref={log}
        role="log"
        aria-live="polite"
        onScroll={(event) => {
          const element = event.currentTarget;

          pinned.current = element.scrollHeight - element.scrollTop - element.clientHeight < 24;
        }}
      >
        {messages.length === 0 ? (
          <div className="chat-drawer__empty">
            <p>Ask about what you're looking at.</p>
            <div className="chat-starters">
              {startersFor(context.kind).map((starter) => (
                <button key={starter} type="button" onClick={() => send(starter)}>
                  {starter}
                </button>
              ))}
            </div>
          </div>
        ) : null}
        {messages.map((message) => {
          if (message.role === "activity")
            return (
              <Activity
                key={message.id}
                icon={message.icon}
                text={message.text}
                note={message.note}
              />
            );

          if (message.role === "approval")
            return message.state === "pending" ? (
              <div key={message.id} className="chat-approval">
                <strong>{message.title}</strong>
                <span>{message.detail}</span>
                <div className="chat-approval__buttons">
                  <button
                    type="button"
                    className="button button--primary"
                    onClick={() => decide(message.id, "allowed")}
                  >
                    Allow
                  </button>
                  <button
                    type="button"
                    className="button"
                    onClick={() => decide(message.id, "denied")}
                  >
                    Deny
                  </button>
                </div>
              </div>
            ) : (
              <Activity
                key={message.id}
                icon={message.state === "allowed" ? "edit" : "denied"}
                text={message.state === "allowed" ? message.title : `Didn't ${message.title}`}
                note={message.state === "allowed" ? "You allowed this" : "You denied this"}
              />
            );

          if (message.role === "assistant")
            return (
              <div key={message.id} className="chat-message chat-message--assistant">
                <p>{message.text}</p>
              </div>
            );

          // A chip only where the context changes, so a thread about one page
          // doesn't repeat it on every message.
          const changed =
            message.context && !sameContext(message.context, previousContext)
              ? message.context
              : null;

          previousContext = message.context;

          return (
            <div key={message.id} className="chat-message chat-message--user">
              {changed ? (
                <div className="chat-message__context">
                  <ContextChip context={changed} />
                </div>
              ) : null}
              <p>{message.text}</p>
            </div>
          );
        })}
        {working ? (
          <div className="chat-activity chat-activity--working">
            <span className="chat-spinner" aria-hidden="true" />
            <span>Working…</span>
          </div>
        ) : null}
      </div>
      <form className="agent-composer chat-composer" onSubmit={onSubmit}>
        <div className="agent-composer__frame">
          <div className="chat-composer__context">
            {attachContext ? (
              <ContextChip context={context} onRemove={() => setDetachedFrom(routeKey)} />
            ) : (
              <button type="button" className="chat-attach" onClick={() => setDetachedFrom(null)}>
                + {context.kind} “{context.label}”
              </button>
            )}
          </div>
          <textarea
            value={draft}
            aria-label="Message"
            placeholder={`Ask about ${context.label}…`}
            rows={2}
            onChange={(event) => setChat({ draft: event.target.value })}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault();
                send(draft);
              }
            }}
          />
          <div className="agent-composer__bar">
            <span
              className="chat-composer__agent"
              title="Claude in auto mode: a reviewer approves routine actions and asks you only about risky ones. Codex uses Approve for me."
            >
              Claude · Opus 5.5 <span className="chat-composer__mode">Auto</span>
            </span>
            {working ? (
              <button type="button" className="button" onClick={stop}>
                Stop
              </button>
            ) : (
              <button
                type="submit"
                className={`button${draft.trim() ? " button--primary" : ""}`}
                disabled={!draft.trim()}
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
