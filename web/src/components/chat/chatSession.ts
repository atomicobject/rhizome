import { useSyncExternalStore } from "react";

// The pane's current chat session and unsent draft. Every section's pane
// shows the same conversation, and the session survives a reload.
const SESSION_KEY = "rhizome:chat:session:v1";

// `sending` holds from a submit until its send returns. It lives here, not in
// the pane, because the pane can unmount and mount again in the meantime.
type ChatPaneState = { sessionID: string; draft: string; sending: boolean };

let state: ChatPaneState | null = null;

const listeners = new Set<() => void>();

function current(): ChatPaneState {
  if (state) return state;

  let sessionID = "";

  try {
    sessionID = window.localStorage.getItem(SESSION_KEY) ?? "";
  } catch {
    // Browser storage is optional; a reload starts a new chat.
  }

  state = { sessionID, draft: "", sending: false };

  return state;
}

function update(change: Partial<ChatPaneState>) {
  const previous = current();
  state = { ...previous, ...change };

  if (state.sessionID !== previous.sessionID) {
    try {
      if (state.sessionID) window.localStorage.setItem(SESSION_KEY, state.sessionID);
      else window.localStorage.removeItem(SESSION_KEY);
    } catch {
      // Browser storage is optional.
    }
  }

  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);

  return () => listeners.delete(listener);
}

export const useChatPane = () => useSyncExternalStore(subscribe, current, current);

export const setChatSession = (sessionID: string) => update({ sessionID });

export const setChatDraft = (draft: string) => update({ draft });

/** Claims the one send in flight; false while another holds it. */
export function beginChatSend(): boolean {
  if (current().sending) return false;
  update({ sending: true });

  return true;
}

export const endChatSend = () => update({ sending: false });

/** Clears the draft only if it is still the text that was sent. */
export function clearChatDraftIf(sent: string) {
  if (current().draft === sent) update({ draft: "" });
}

/** Starts over: the next message opens a new session. The old one stays on the Agent page. */
export const startNewChat = () => update({ sessionID: "", draft: "" });

/** Forget the cached state; tests call this after changing storage. */
export function resetChatPaneForTests() {
  state = null;
}
