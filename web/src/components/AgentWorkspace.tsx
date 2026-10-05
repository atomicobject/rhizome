import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  createAgentSession,
  deleteAgentSession,
  getAgentEventsUrl,
  getAgentSession,
  getAgentSettings,
  interruptAgentSession,
  listAgentSessions,
  respondToAgentApproval,
  saveAgentSettings,
  sendAgentMessage,
} from "../api/internalAgentClient";
import { openReconnectingEventSource } from "../api/eventStream";
import { decodeJson, isFiniteNumber, isJsonObject, isOptionalString, isString } from "../api/parse";
import { queryKeys } from "../api/queryKeys";
import type {
  AgentApprovalDecision,
  AgentEvent,
  AgentMessage,
  AgentSession,
  AgentSessionResponse,
  AgentSessionsResponse,
  AgentSettings,
  AgentSettingsResponse,
} from "../api/types";
import { AgentSessionRail } from "./agent/AgentSessionRail";
import { AgentStatusHeader } from "./agent/AgentStatusHeader";
import { AgentTimeline } from "./agent/AgentTimeline";

const defaultSettings: AgentSettings = {
  harnesses: {
    codex: { permissionMode: "approval-required" },
    claude: { permissionMode: "approval-required" },
  },
};

const emptySettingsResponse: AgentSettingsResponse = {
  settings: defaultSettings,
  resolved: { reason: "none available" },
  harnesses: [],
};

function mergeEvents(current: AgentEvent[], incoming: AgentEvent[]) {
  const byID = new Map<number, AgentEvent>();

  for (const event of current) byID.set(event.id, event);

  for (const event of incoming) byID.set(event.id, event);

  return [...byID.values()].sort(
    (left, right) =>
      new Date(left.createdAt).getTime() - new Date(right.createdAt).getTime() ||
      left.id - right.id,
  );
}

function mergeMessages(current: AgentMessage[], incoming: AgentMessage[]) {
  const persisted = new Set(
    incoming.flatMap((message) => (message.id > 0 ? [`${message.role}:${message.content}`] : [])),
  );

  const byID = new Map<number, AgentMessage>();

  for (const message of current) {
    if (message.id < 0 && persisted.has(`${message.role}:${message.content}`)) continue;
    byID.set(message.id, message);
  }

  for (const message of incoming) byID.set(message.id, message);

  return [...byID.values()].sort(
    (left, right) =>
      new Date(left.createdAt).getTime() - new Date(right.createdAt).getTime() ||
      left.id - right.id,
  );
}

function isAgentEvent(value: unknown): value is AgentEvent {
  if (!isJsonObject(value)) return false;

  return (
    isFiniteNumber(value.id) &&
    isString(value.sessionId) &&
    isString(value.type) &&
    isString(value.createdAt) &&
    isOptionalString(value.role) &&
    isOptionalString(value.content) &&
    isOptionalString(value.toolName) &&
    isOptionalString(value.error)
  );
}

function completedAssistant(events: AgentEvent[], completed: AgentEvent): AgentMessage | undefined {
  if (completed.type !== "turn-completed") return undefined;
  const turnID = completed.result?.turnId;

  const content = events
    .flatMap((event) =>
      (event.type === "message_delta" || event.type === "assistant-text-delta") &&
      (!turnID || event.result?.turnId === turnID)
        ? [event.content || ""]
        : [],
    )
    .join("");

  if (!content.trim()) return undefined;

  return {
    id: -Math.max(1, completed.id),
    sessionId: completed.sessionId,
    role: "assistant",
    content,
    createdAt: completed.createdAt,
  };
}

export function AgentWorkspace() {
  const queryClient = useQueryClient();
  const [selectedID, setSelectedID] = useState("");

  const [thread, setThread] = useState<{
    sessionID: string;
    messages: AgentMessage[];
    events: AgentEvent[];
  }>({ sessionID: "", messages: [], events: [] });

  const [draft, setDraft] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [error, setError] = useState("");
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [deleteConfirmID, setDeleteConfirmID] = useState("");
  const threadRef = useRef<HTMLDivElement | null>(null);
  const latestEventIDRef = useRef(0);

  const sessionsQuery = useQuery({
    queryKey: queryKeys.agent.sessions(),
    queryFn: ({ signal }) => listAgentSessions({ signal }),
  });

  const settingsQuery = useQuery({
    queryKey: queryKeys.agent.settings(),
    queryFn: ({ signal }) => getAgentSettings({ signal }),
  });

  const sessionQuery = useQuery({
    queryKey: queryKeys.agent.session(selectedID),
    queryFn: ({ signal }) => getAgentSession(selectedID, { signal }),
    enabled: Boolean(selectedID),
  });

  const sessions = sessionsQuery.data?.sessions ?? [];
  const settingsResponse = settingsQuery.data ?? emptySettingsResponse;
  const messages = thread.sessionID === selectedID ? thread.messages : [];
  const events = thread.sessionID === selectedID ? thread.events : [];

  const selectedSession =
    sessionQuery.data?.session.id === selectedID
      ? sessionQuery.data.session
      : sessions.find((session) => session.id === selectedID) || null;

  const setSessions = useCallback(
    (updater: (current: AgentSession[]) => AgentSession[]) => {
      return queryClient.setQueryData<AgentSessionsResponse>(
        queryKeys.agent.sessions(),
        (current) => ({
          ...current,
          sessions: updater(current?.sessions ?? []),
        }),
      );
    },
    [queryClient],
  );

  const updateSession = useCallback(
    (sessionID: string, update: Partial<AgentSession>) => {
      setSessions((current) =>
        current.map((session) => (session.id === sessionID ? { ...session, ...update } : session)),
      );
      queryClient.setQueryData<AgentSessionResponse>(
        queryKeys.agent.session(sessionID),
        (current) =>
          current ? { ...current, session: { ...current.session, ...update } } : current,
      );
    },
    [queryClient, setSessions],
  );

  const settingsMutation = useMutation({
    mutationFn: saveAgentSettings,
    scope: { id: "agent-settings" },
    onSuccess: (response) => queryClient.setQueryData(queryKeys.agent.settings(), response),
  });

  useEffect(() => {
    if (sessions.length > 0) setSelectedID((current) => current || sessions[0]?.id || "");
  }, [sessions]);

  useEffect(() => {
    if (!selectedID) {
      setThread({ sessionID: "", messages: [], events: [] });
      latestEventIDRef.current = 0;

      return;
    }

    if (!sessionQuery.data) return;
    const incomingEvents = sessionQuery.data.events ?? [];
    setThread((current) => ({
      sessionID: selectedID,
      messages: mergeMessages(
        current.sessionID === selectedID ? current.messages : [],
        sessionQuery.data.messages ?? [],
      ),
      events: mergeEvents(current.sessionID === selectedID ? current.events : [], incomingEvents),
    }));
    setSessions((current) =>
      current.map((session) =>
        session.id === selectedID ? { ...session, ...sessionQuery.data.session } : session,
      ),
    );
    latestEventIDRef.current = Math.max(
      latestEventIDRef.current,
      ...incomingEvents.map((event) => event.id),
      0,
    );
  }, [selectedID, sessionQuery.data, setSessions]);

  useEffect(() => {
    if (!selectedID) return;

    const handleEvent = (message: Event) => {
      if (!(message instanceof MessageEvent)) return;
      const event = decodeJson(String(message.data), isAgentEvent);

      if (!event || event.sessionId !== selectedID) return;

      if (event.id > 0) {
        latestEventIDRef.current = Math.max(latestEventIDRef.current, event.id);
      }

      setThread((current) => {
        const currentEvents = current.sessionID === selectedID ? current.events : [];
        const nextEvents = mergeEvents(currentEvents, [event]);
        const assistant = completedAssistant(nextEvents, event);

        return {
          sessionID: selectedID,
          events: nextEvents,
          messages: assistant
            ? mergeMessages(current.sessionID === selectedID ? current.messages : [], [assistant])
            : current.sessionID === selectedID
              ? current.messages
              : [],
        };
      });

      if (event.type === "turn-started") updateSession(selectedID, { turnRunning: true });

      if (
        event.type === "turn-completed" ||
        (event.type === "error" && event.result?.status !== "retrying")
      ) {
        updateSession(selectedID, { turnRunning: false });
        queryClient.invalidateQueries({ queryKey: queryKeys.agent.session(selectedID) });
      }
    };

    let removeListener = () => {};

    const stop = openReconnectingEventSource(
      () => getAgentEventsUrl(selectedID, latestEventIDRef.current),
      (source) => {
        removeListener();
        source.addEventListener("agent_event", handleEvent);
        removeListener = () => source.removeEventListener("agent_event", handleEvent);
      },
    );

    return () => {
      removeListener();
      stop();
    };
  }, [queryClient, selectedID, updateSession]);

  useEffect(() => {
    if (threadRef.current) threadRef.current.scrollTop = threadRef.current.scrollHeight;
  }, [events, messages]);

  useEffect(() => {
    if (!deleteConfirmID) return;

    const reset = (event: PointerEvent) => {
      const target = event.target instanceof Element ? event.target : null;

      if (!target?.closest(".agent-session__delete")) setDeleteConfirmID("");
    };

    document.addEventListener("pointerdown", reset);

    return () => document.removeEventListener("pointerdown", reset);
  }, [deleteConfirmID]);

  const startSession = async () => {
    try {
      const response = await createAgentSession("Rhizome agent chat");
      setSessions((current) => [response.session, ...current]);
      setSelectedID(response.session.id);
      setDrawerOpen(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  };

  const deleteSession = async (sessionID: string) => {
    if (deleteConfirmID !== sessionID) {
      setDeleteConfirmID(sessionID);

      return;
    }

    try {
      await deleteAgentSession(sessionID);
      const next = setSessions((current) => current.filter((session) => session.id !== sessionID));
      setSelectedID((current) => (current === sessionID ? next?.sessions?.[0]?.id || "" : current));
      setDeleteConfirmID((current) => (current === sessionID ? "" : current));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    }
  };

  const submitText = async (content: string) => {
    const trimmed = content.trim();

    if (!trimmed || isSending || selectedSession?.turnRunning || selectedSession?.readOnly) return;
    setError("");
    setIsSending(true);
    let sessionID = selectedID;

    try {
      if (!sessionID) {
        const created = await createAgentSession(trimmed.slice(0, 48));
        sessionID = created.session.id;
        setSessions((current) => [created.session, ...current]);
        setSelectedID(sessionID);
      }

      const optimistic: AgentMessage = {
        id: -Date.now(),
        sessionId: sessionID,
        role: "user",
        content: trimmed,
        createdAt: new Date().toISOString(),
      };

      setThread((current) => ({
        sessionID,
        messages: mergeMessages(current.sessionID === sessionID ? current.messages : [], [
          optimistic,
        ]),
        events: current.sessionID === sessionID ? current.events : [],
      }));
      setDraft("");
      updateSession(sessionID, { turnRunning: true });
      const response = await sendAgentMessage(sessionID, trimmed);
      updateSession(sessionID, response.session);
    } catch (cause) {
      if (sessionID) updateSession(sessionID, { turnRunning: false });
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setIsSending(false);
    }
  };

  const harness =
    selectedSession?.harness ||
    settingsResponse.settings.harness ||
    settingsResponse.resolved.harness;

  const status = settingsResponse.harnesses.find((item) => item.kind === harness)?.status;
  const busy = isSending || Boolean(selectedSession?.turnRunning);

  const disabledReason = selectedSession?.readOnly
    ? "This pre-harness session is read-only."
    : busy
      ? "A turn is running. Interrupt it before sending another message."
      : "";

  const readError = [sessionsQuery.error, settingsQuery.error, sessionQuery.error].find(Boolean);

  const visibleError =
    error || (readError instanceof Error ? readError.message : readError ? String(readError) : "");

  return (
    <main
      className={drawerOpen ? "agent-workspace agent-workspace--drawer-open" : "agent-workspace"}
    >
      {drawerOpen ? (
        <button
          type="button"
          className="agent-drawer-scrim"
          aria-label="Close agent sidebar"
          onClick={() => setDrawerOpen(false)}
        />
      ) : null}
      <AgentSessionRail
        sessions={sessions}
        selectedID={selectedID}
        loading={sessionsQuery.isPending}
        error={sessionsQuery.isError}
        deleteConfirmID={deleteConfirmID}
        settings={settingsResponse}
        settingsSaving={settingsMutation.isPending}
        onSelect={(id) => {
          setDeleteConfirmID("");
          setSelectedID(id);
          setDrawerOpen(false);
        }}
        onNew={startSession}
        onDelete={deleteSession}
        onRetry={() => sessionsQuery.refetch()}
        onSettingsChange={(settings) => {
          settingsMutation.mutate(settings, {
            onError: (cause) => setError(cause instanceof Error ? cause.message : String(cause)),
          });
        }}
      />
      <section className="agent-thread" aria-label="Agent chat">
        <AgentStatusHeader
          session={selectedSession}
          harness={harness}
          status={status}
          resolutionReason={settingsResponse.resolved.reason}
          messageCount={messages.length}
          drawerOpen={drawerOpen}
          onToggleDrawer={() => setDrawerOpen((open) => !open)}
        />
        {selectedSession?.readOnly ? (
          <p className="agent-error" role="status">
            Read-only history from before harness sessions were introduced.
          </p>
        ) : null}
        <div className="agent-thread__body" ref={threadRef}>
          <AgentTimeline
            messages={messages}
            events={events}
            onApproval={async (requestID, decision: AgentApprovalDecision) => {
              try {
                await respondToAgentApproval(selectedID, requestID, decision);
              } catch (cause) {
                setError(cause instanceof Error ? cause.message : String(cause));
                throw cause;
              }
            }}
          />
        </div>
        {visibleError ? (
          <p className="agent-error" role="alert">
            {visibleError}
          </p>
        ) : null}
        {selectedSession?.turnRunning ? (
          <button
            type="button"
            onClick={async () => {
              try {
                await interruptAgentSession(selectedSession.id);
              } catch (cause) {
                setError(cause instanceof Error ? cause.message : String(cause));
              }
            }}
          >
            Interrupt
          </button>
        ) : null}
        <form
          className="agent-composer"
          onSubmit={(event) => {
            event.preventDefault();
            submitText(draft);
          }}
        >
          <div>
            <textarea
              value={draft}
              aria-label="Message"
              placeholder="Ask Rhizome…"
              rows={3}
              disabled={busy || Boolean(selectedSession?.readOnly)}
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                  event.preventDefault();
                  submitText(draft);
                }
              }}
            />
            {disabledReason ? <p className="agent-muted">{disabledReason}</p> : null}
          </div>
          <button
            type="submit"
            disabled={busy || !draft.trim() || Boolean(selectedSession?.readOnly)}
          >
            Send
          </button>
        </form>
      </section>
    </main>
  );
}
