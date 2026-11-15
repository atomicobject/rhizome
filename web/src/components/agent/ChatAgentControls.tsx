// The composer's agent controls (SPEC-0140): one picker for the model and its
// reasoning effort and a permission mode menu, which the right pane and the
// Agent page share. A choice saves the agent and model new chats use.
import { type QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState, useSyncExternalStore } from "react";

import { getAgentSettings, saveAgentSettings } from "../../api/internalAgentClient";
import { queryKeys } from "../../api/queryKeys";
import type { AgentSession, AgentSettingsResponse } from "../../api/types";
import {
  agentGroups,
  blockerFor,
  type ChatChoice,
  choiceLabel,
  currentChoice,
  emptySettingsResponse,
  HARNESS_LABELS,
  PERMISSION_MODES,
  readiness,
  statusOf,
  withChoice,
  inChatComposer,
} from "./chatAgent";
import { commandKey, IS_MAC, ModeMenu, ModelPicker } from "./ChatAgentPanels";

const shortcut = (key: string) => (IS_MAC ? `⇧⌘${key}` : `Ctrl+Shift+${key}`);

/** Saves and "Check again" run one at a time, so the last one started lands last. */
const AGENT_SETTINGS_SCOPE = { id: "agent-settings" };

// The last working choice before one that left chat blocked, so the notice
// can offer it back. It lasts as long as the page, across both composers.
let previous: ChatChoice | undefined;

const previousListeners = new Set<() => void>();

function rememberPrevious(choice: ChatChoice) {
  previous = choice;
  previousListeners.forEach((listener) => listener());
}

const subscribePrevious = (listener: () => void) => {
  previousListeners.add(listener);

  return () => previousListeners.delete(listener);
};

// Each app's last choice save. The server runs the saved choice, so a
// message sent before it lands would run the one before it.
const saving = new WeakMap<QueryClient, Promise<boolean>>();

// Choices still saving. A reply that lands while a later choice waits would
// show, and then save, the earlier choice, so only the last reply is stored.
const unsaved = new WeakMap<QueryClient, number>();

const pendingChoices = (queryClient: QueryClient) => unsaved.get(queryClient) ?? 0;

export function resetChatAgentForTests() {
  previous = undefined;
}

export type ChatAgent = ReturnType<typeof useChatAgent>;

/** Chat's settings, the choice they make, and what keeps a message from sending; `session` is the open conversation. */
export function useChatAgent(session?: Pick<AgentSession, "harness"> | null) {
  const queryClient = useQueryClient();
  const key = queryKeys.agent.settings();

  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => getAgentSettings({ signal }),
    // A harness that was busy or slow to answer its probe may answer later.
    refetchInterval: (state) => (state.state.data?.resolved.harness ? false : 15_000),
  });

  const response = query.data ?? emptySettingsResponse;
  const choice = currentChoice(response);
  // Until settings load, nothing is known to be missing.
  const blocker = query.isSuccess ? blockerFor(response, choice) : undefined;

  // A reply lands after cancelling reads still in flight, which began before
  // it and would otherwise land after it with the old choice.
  const store = async (next: AgentSettingsResponse) => {
    await queryClient.cancelQueries({ queryKey: key });
    queryClient.setQueryData(key, next);
  };

  const save = useMutation({
    mutationFn: (next: ChatChoice) => {
      const latest = queryClient.getQueryData<AgentSettingsResponse>(key) ?? response;

      return saveAgentSettings(withChoice(latest.settings, next));
    },
    scope: AGENT_SETTINGS_SCOPE,
    // The composer follows the click, not the save's reply.
    onMutate: async (next) => {
      unsaved.set(queryClient, pendingChoices(queryClient) + 1);
      await queryClient.cancelQueries({ queryKey: key });
      queryClient.setQueryData<AgentSettingsResponse>(
        key,
        (current) => current && { ...current, settings: withChoice(current.settings, next) },
      );
    },
    onSuccess: async (next) => {
      if (pendingChoices(queryClient) === 1) await store(next);
    },
    onError: () => queryClient.invalidateQueries({ queryKey: key }),
    onSettled: () => unsaved.set(queryClient, pendingChoices(queryClient) - 1),
  });

  const recheck = useMutation({
    mutationFn: () => getAgentSettings({ refresh: true }),
    scope: AGENT_SETTINGS_SCOPE,
    onSuccess: async (next) => {
      if (pendingChoices(queryClient) === 0) await store(next);
    },
  });

  const remembered = useSyncExternalStore(subscribePrevious, () => previous);

  const fallback =
    blocker &&
    remembered &&
    !response.settings.harnesses[remembered.harness]?.disabled &&
    readiness(statusOf(response, remembered.harness)) === "ready"
      ? remembered
      : undefined;

  return {
    response,
    choice,
    blocker,
    readError: query.error,
    error: save.error ?? recheck.error,
    checking: recheck.isPending,
    /** A working choice from before the one that blocked chat. */
    fallback,
    /** The open conversation runs another agent, so the next message starts a new chat. */
    newChat: Boolean(session?.harness && choice && session.harness !== choice.harness),
    choose: (next: ChatChoice) => {
      if (choice && !blocker) rememberPrevious(choice);

      // Saves run in order, so the last one settles after every earlier one.
      const pending = save.mutateAsync(next).then(
        () => true,
        () => false,
      );

      saving.set(queryClient, pending);
      // A failed save holds only the sends that waited on it; settings reload
      // after it, so later sends go ahead with the choice the server kept.
      void pending.then(() => {
        if (saving.get(queryClient) === pending) saving.delete(queryClient);
      });
    },
    /**
     * Waits until the latest choice is saved, including one made while waiting;
     * false when its save failed or it needs sign-in or an install, which the notice reports.
     */
    saved: async () => {
      let ok = true;

      for (let pending = saving.get(queryClient); pending; pending = saving.get(queryClient)) {
        ok = await pending;

        if (saving.get(queryClient) === pending) break;
      }

      if (!ok) return false;

      // A choice made while waiting may need sign-in or an install; its notice holds the message.
      const latest = queryClient.getQueryData<AgentSettingsResponse>(key) ?? response;

      return !blockerFor(latest, currentChoice(latest));
    },
    /**
     * Whether the next message starts a new chat, read after `saved` from the
     * saved choice; `harness` is the open chat's agent once it has loaded.
     */
    startsNewChat: (harness = session?.harness) => {
      const saved = currentChoice(queryClient.getQueryData<AgentSettingsResponse>(key) ?? response);

      return Boolean(harness && saved && harness !== saved.harness);
    },
    recheck: () => recheck.mutate(),
  };
}

function Chevron() {
  return (
    <svg aria-hidden="true" viewBox="0 0 10 10">
      <path d="M2 6.5 5 3.5 8 6.5" fill="none" stroke="currentColor" strokeWidth="1.4" />
    </svg>
  );
}

export type Open = "" | "models" | "reasoning" | "modes";

/** ⇧⌘M opens the picker, ⇧⌘E its Reasoning column, and ⇧⌘A the mode menu. */
function shortcutPanel(code: string): Open {
  switch (code) {
    case "KeyM":
      return "models";
    case "KeyE":
      return "reasoning";
    case "KeyA":
      return "modes";
    default:
      return "";
  }
}

/** The model and reasoning picker and the permission mode menu, in the composer's bar. */
export function ChatAgentControls({ agent }: { agent: ChatAgent }) {
  const [open, setOpen] = useState<Open>("");
  const modelButton = useRef<HTMLButtonElement>(null);
  const modeButton = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef<HTMLElement | null>(null);
  const openRef = useRef<Open>("");
  const { choice, response } = agent;

  const show = (which: Open) => {
    // Closing returns focus to where it was before the first panel opened.
    if (!openRef.current && document.activeElement instanceof HTMLElement)
      returnFocus.current = document.activeElement;
    openRef.current = which;
    setOpen(which);
  };

  const close = (restoreFocus: boolean) => {
    openRef.current = "";
    setOpen("");

    if (restoreFocus) returnFocus.current?.focus();
  };

  const hasChoice = Boolean(choice);
  // A fresh setup has no choice yet, but the picker still offers each agent that is on.
  const hasPicker = agentGroups(response).length > 0;

  useEffect(() => {
    if (!hasPicker) return;

    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const which = shortcutPanel(event.code);

      if (!which || !event.shiftKey || event.altKey || !commandKey(event)) return;

      if (which === "modes" && !hasChoice) return;

      if (which === "reasoning" && !inChatComposer(event.target)) return;

      event.preventDefault();
      show(which);
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, [hasPicker, hasChoice]);

  if (!hasPicker) return null;

  const label = choice ? choiceLabel(response, choice) : { model: "Choose a model", effort: "" };
  const named = label.effort ? `${label.model} · ${label.effort}` : label.model;
  const mode = choice && PERMISSION_MODES.find((item) => item.mode === choice.permissionMode);
  const pickerOpen = open === "models" || open === "reasoning";

  return (
    <div className="chat-agent-controls">
      <button
        ref={modelButton}
        type="button"
        className="chat-agent-trigger"
        aria-haspopup="dialog"
        aria-expanded={pickerOpen}
        aria-label={named}
        aria-keyshortcuts={IS_MAC ? "Shift+Meta+M" : "Control+Shift+M"}
        title={`${choice ? `${HARNESS_LABELS[choice.harness]} · ` : ""}${named} (${shortcut("M")})`}
        onClick={() => (pickerOpen ? close(false) : show("models"))}
      >
        <span className="chat-agent-trigger__text">
          {label.model}
          {label.effort ? (
            <>
              <span className="chat-agent-trigger__sep"> · </span>
              {label.effort}
            </>
          ) : null}
        </span>
        <Chevron />
      </button>
      {choice ? (
        <button
          ref={modeButton}
          type="button"
          className="chat-agent-trigger"
          aria-haspopup="menu"
          aria-expanded={open === "modes"}
          aria-keyshortcuts={IS_MAC ? "Shift+Meta+A" : "Control+Shift+A"}
          title={`Permission mode (${shortcut("A")})`}
          onClick={() => (open === "modes" ? close(false) : show("modes"))}
        >
          {mode?.label ?? choice.permissionMode}
          <Chevron />
        </button>
      ) : null}
      {pickerOpen ? (
        <ModelPicker
          agent={agent}
          choice={choice}
          anchor={modelButton}
          reasoning={open === "reasoning"}
          onClose={close}
        />
      ) : null}
      {open === "modes" && choice ? (
        <ModeMenu agent={agent} choice={choice} anchor={modeButton} onClose={close} />
      ) : null}
    </div>
  );
}
