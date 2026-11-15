import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";

import { openReconnectingEventSource } from "../../api/eventStream";
import {
  getAgentEventsUrl,
  getAgentSession,
  sendAgentMessage,
} from "../../api/internalAgentClient";
import {
  decodeJson,
  isFiniteNumber,
  isJsonObject,
  isOptionalString,
  isString,
} from "../../api/parse";
import { queryKeys } from "../../api/queryKeys";
import type {
  AgentEvent,
  AgentMessage,
  AgentSession,
  AgentSessionResponse,
  AgentSessionsResponse,
  AgentViewContext,
} from "../../api/types";
import { joinDeltas } from "./AgentTimeline";
import { answerPageRequest, type PageIdentity } from "./pageText";

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

  const content = joinDeltas(events.filter((event) => !turnID || event.result?.turnId === turnID));

  if (!content.trim()) return undefined;

  return {
    id: -Math.max(1, completed.id),
    sessionId: completed.sessionId,
    role: "assistant",
    content,
    createdAt: completed.createdAt,
  };
}

type Thread = { sessionID: string; messages: AgentMessage[]; events: AgentEvent[] };

/**
 * One agent chat session's live thread: its stored messages and events, the
 * SSE stream that extends them, and sending. The Agent page and the right
 * pane's Chat tab share it. While subscribed, it answers the agent's page_text
 * requests with the page `describePage` names (SPEC-0140).
 */
export function useAgentThread(sessionID: string, describePage: () => PageIdentity) {
  const queryClient = useQueryClient();
  const [thread, setThread] = useState<Thread>({ sessionID: "", messages: [], events: [] });
  const [isSending, setIsSending] = useState(false);
  // The session whose send reply is pending; the server may not know its turn yet.
  const [sendingID, setSendingID] = useState("");
  // The session whose turn this window started and has not seen end.
  const [startedID, setStartedID] = useState("");
  const latestEventIDRef = useRef(0);
  // Counts terminal turn events per session, so a slow send reply cannot revive a finished turn.
  const terminalEventsRef = useRef(new Map<string, number>());
  const describePageRef = useRef(describePage);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;

    return () => {
      mounted.current = false;
    };
  }, []);

  describePageRef.current = describePage;

  const sessionQuery = useQuery({
    queryKey: queryKeys.agent.session(sessionID),
    queryFn: ({ signal }) => getAgentSession(sessionID, { signal }),
    enabled: Boolean(sessionID),
  });

  const updateListed = useCallback(
    (id: string, update: Partial<AgentSession>) =>
      queryClient.setQueryData<AgentSessionsResponse>(queryKeys.agent.sessions(), (current) =>
        current
          ? {
              ...current,
              sessions: (current.sessions ?? []).map((session) =>
                session.id === id ? { ...session, ...update } : session,
              ),
            }
          : current,
      ),
    [queryClient],
  );

  const updateSession = useCallback(
    (id: string, update: Partial<AgentSession>) => {
      updateListed(id, update);
      queryClient.setQueryData<AgentSessionResponse>(queryKeys.agent.session(id), (current) =>
        current ? { ...current, session: { ...current.session, ...update } } : current,
      );
    },
    [queryClient, updateListed],
  );

  useEffect(() => {
    if (!sessionID) {
      setThread({ sessionID: "", messages: [], events: [] });
      latestEventIDRef.current = 0;

      return;
    }

    if (!sessionQuery.data) return;
    const incoming = sessionQuery.data;
    const incomingEvents = incoming.events ?? [];
    setThread((current) => ({
      sessionID,
      messages: mergeMessages(
        current.sessionID === sessionID ? current.messages : [],
        incoming.messages ?? [],
      ),
      events: mergeEvents(current.sessionID === sessionID ? current.events : [], incomingEvents),
    }));
    updateListed(sessionID, incoming.session);
    latestEventIDRef.current = Math.max(
      latestEventIDRef.current,
      ...incomingEvents.map((event) => event.id),
      0,
    );
  }, [sessionID, sessionQuery.data, updateListed]);

  useEffect(() => {
    if (!sessionID) return;

    const handleEvent = (message: Event) => {
      if (!(message instanceof MessageEvent)) return;
      const event = decodeJson(String(message.data), isAgentEvent);

      if (!event || event.sessionId !== sessionID) return;

      // A transient request from the agent's page_text tool, not thread history.
      if (event.type === "page-request") {
        const requestID = event.result?.requestId;

        if (requestID) void answerPageRequest(sessionID, requestID, describePageRef.current());

        return;
      }

      if (event.id > 0) {
        latestEventIDRef.current = Math.max(latestEventIDRef.current, event.id);
      }

      setThread((current) => {
        const currentEvents = current.sessionID === sessionID ? current.events : [];
        const nextEvents = mergeEvents(currentEvents, [event]);
        const assistant = completedAssistant(nextEvents, event);

        return {
          sessionID,
          events: nextEvents,
          messages: assistant
            ? mergeMessages(current.sessionID === sessionID ? current.messages : [], [assistant])
            : current.sessionID === sessionID
              ? current.messages
              : [],
        };
      });

      if (event.type === "turn-started") updateSession(sessionID, { turnRunning: true });

      if (
        event.type === "turn-completed" ||
        (event.type === "error" && event.result?.status !== "retrying")
      ) {
        terminalEventsRef.current.set(
          sessionID,
          (terminalEventsRef.current.get(sessionID) ?? 0) + 1,
        );
        setStartedID((current) => (current === sessionID ? "" : current));
        updateSession(sessionID, { turnRunning: false });
        queryClient.invalidateQueries({ queryKey: queryKeys.agent.session(sessionID) });
      }
    };

    let removeListener = () => {};

    const stop = openReconnectingEventSource(
      () => getAgentEventsUrl(sessionID, latestEventIDRef.current),
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
  }, [queryClient, sessionID, updateSession]);

  /** Sends to `targetID`, which may be a session created for this message. Throws on failure. */
  const send = async (targetID: string, content: string, context?: AgentViewContext) => {
    const optimistic: AgentMessage = {
      id: -Date.now(),
      sessionId: targetID,
      role: "user",
      content,
      context,
      createdAt: new Date().toISOString(),
    };

    setThread((current) => ({
      sessionID: targetID,
      messages: mergeMessages(current.sessionID === targetID ? current.messages : [], [optimistic]),
      events: current.sessionID === targetID ? current.events : [],
    }));
    setIsSending(true);
    setStartedID(targetID);
    updateSession(targetID, { turnRunning: true });
    setSendingID(targetID);
    const terminalsBefore = terminalEventsRef.current.get(targetID) ?? 0;

    try {
      const response = await sendAgentMessage(targetID, content, context);
      const { turnRunning: _stale, ...settled } = response.session;
      const turnEnded = (terminalEventsRef.current.get(targetID) ?? 0) !== terminalsBefore;
      // A thread that unmounted while the reply was on its way stopped hearing
      // the stream, so it cannot know the turn is still running.
      updateSession(targetID, turnEnded || !mounted.current ? settled : response.session);

      if (turnEnded) setStartedID("");
    } catch (cause) {
      setStartedID("");
      updateSession(targetID, { turnRunning: false });
      throw cause;
    } finally {
      setIsSending(false);
      setSendingID("");
    }
  };

  const current = thread.sessionID === sessionID;

  return {
    sessionQuery,
    messages: current ? thread.messages : [],
    events: current ? thread.events : [],
    isSending,
    sendingID,
    /** A turn runs: one this window started, or one the server reports. */
    turnRunning:
      (Boolean(sessionID) && startedID === sessionID) ||
      Boolean(sessionQuery.data?.session.turnRunning),
    send,
  };
}
