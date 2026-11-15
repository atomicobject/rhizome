import { AgentComposer } from "./agent/AgentComposer";
import { ExamplePrompts } from "./agent/ExamplePrompts";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  createAgentSession,
  deleteAgentSession,
  getAgentSettings,
  listAgentSessions,
  respondToAgentApproval,
  saveAgentSettings,
} from "../api/internalAgentClient";
import { queryKeys } from "../api/queryKeys";
import type { AgentApprovalDecision, AgentSession, AgentSessionsResponse } from "../api/types";
import { AgentSetup, chatModelLabel, needsHarnessChoice } from "./agent/AgentSetup";
import { emptySettingsResponse } from "./agent/AgentSettingsPanel";
import { AgentSessionRail } from "./agent/AgentSessionRail";
import { AgentStatusHeader, harnessBlocked } from "./agent/AgentStatusHeader";
import { AgentTimeline } from "./agent/AgentTimeline";
import { useAgentThread } from "./agent/useAgentThread";
import { WorkbenchSidebar } from "./WorkbenchSidebar";

/** The session a link asked for, as `/agent?session=<id>`. */
function linkedSession() {
  return new URLSearchParams(window.location.search).get("session") || "";
}

const describeAgentPage = () => ({
  title: "Agent",
  route: `${window.location.pathname}${window.location.search}`,
});

export function AgentWorkspace({
  sidebarTitle,
  sidebarHint,
}: {
  sidebarTitle?: string | null;
  sidebarHint?: string | null;
}) {
  const queryClient = useQueryClient();
  const [selectedID, setSelectedID] = useState(linkedSession);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
  const [deleteConfirmID, setDeleteConfirmID] = useState("");
  const threadRef = useRef<HTMLDivElement | null>(null);
  const composerRef = useRef<HTMLTextAreaElement | null>(null);
  const submitting = useRef(false);

  const sessionsQuery = useQuery({
    queryKey: queryKeys.agent.sessions(),
    queryFn: ({ signal }) => listAgentSessions({ signal }),
  });

  const settingsQuery = useQuery({
    queryKey: queryKeys.agent.settings(),
    queryFn: ({ signal }) => getAgentSettings({ signal }),
  });

  const { sessionQuery, messages, events, isSending, sendingID, send } = useAgentThread(
    selectedID,
    describeAgentPage,
  );

  const sessions = sessionsQuery.data?.sessions ?? [];
  const settingsResponse = settingsQuery.data ?? emptySettingsResponse;

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

  const settingsMutation = useMutation({
    mutationFn: saveAgentSettings,
    scope: { id: "agent-settings" },
    onSuccess: (response) => queryClient.setQueryData(queryKeys.agent.settings(), response),
  });

  useEffect(() => {
    if (sessions.length > 0) setSelectedID((current) => current || sessions[0]?.id || "");
  }, [sessions]);

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

    if (
      !trimmed ||
      blocked ||
      isSending ||
      submitting.current ||
      selectedSession?.turnRunning ||
      selectedSession?.readOnly
    )
      return;
    // Held across a new session's creation, so a second submit in that wait
    // cannot create another session and send the message twice.
    submitting.current = true;
    setError("");
    let sessionID = selectedID;

    try {
      if (!sessionID) {
        const created = await createAgentSession(trimmed.slice(0, 48));
        sessionID = created.session.id;
        setSessions((current) => [created.session, ...current]);
        setSelectedID(sessionID);
      }

      setDraft("");
      await send(sessionID, trimmed);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      submitting.current = false;
    }
  };

  const harness =
    selectedSession?.harness ||
    settingsResponse.settings.harness ||
    settingsResponse.resolved.harness;

  const status = settingsResponse.harnesses.find((item) => item.kind === harness)?.status;

  const model = chatModelLabel(settingsResponse, harness, selectedSession?.model);

  const busy = isSending || Boolean(selectedSession?.turnRunning);

  // Until settings load, the harness is unknown rather than missing.
  const blocked =
    !selectedSession?.readOnly && settingsQuery.isSuccess && harnessBlocked(harness, status);

  const disabledReason = selectedSession?.readOnly
    ? "This pre-harness session is read-only."
    : busy
      ? "A turn is running. Interrupt it before sending another message."
      : "";

  const readError = [sessionsQuery.error, settingsQuery.error, sessionQuery.error].find(Boolean);

  const visibleError =
    error || (readError instanceof Error ? readError.message : readError ? String(readError) : "");

  const empty = messages.length === 0 && events.length === 0;

  // Blocked, or on first use with more than one harness ready, so the person
  // picks once. A session keeps the harness it began with.
  const setup =
    settingsQuery.isSuccess &&
    !selectedSession?.readOnly &&
    (blocked || (!selectedSession && empty && needsHarnessChoice(settingsResponse)));

  const showExamples = empty && !setup && !selectedSession?.readOnly;

  return (
    <main className="workspace workbench agent-workspace">
      <WorkbenchSidebar title={sidebarTitle} titleHint={sidebarHint}>
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
      </WorkbenchSidebar>
      <section
        className={`workbench__panel agent-thread${showExamples || (setup && empty) ? " agent-thread--empty" : ""}`}
        aria-label="Agent chat"
      >
        <AgentStatusHeader
          session={selectedSession}
          harness={harness}
          status={status}
          resolutionReason={settingsResponse.resolved.reason}
          messageCount={messages.length}
          model={model}
        />
        {selectedSession?.readOnly ? (
          <p className="agent-error" role="status">
            Read-only history from before harness sessions were introduced.
          </p>
        ) : null}
        <div className="agent-thread__body" ref={threadRef}>
          {setup ? <AgentSetup response={settingsResponse} blocked={blocked} /> : null}
          {showExamples ? (
            <ExamplePrompts
              onPick={(text) => {
                setDraft(text);
                composerRef.current?.focus();
              }}
            />
          ) : null}
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
        <AgentComposer
          // Until its send reply, the server may not have begun the turn, and
          // would refuse an interrupt.
          runningID={
            selectedSession?.turnRunning && sendingID !== selectedSession.id
              ? selectedSession.id
              : ""
          }
          onError={setError}
          draft={draft}
          onChange={setDraft}
          onSubmit={submitText}
          composerRef={composerRef}
          busy={busy}
          blocked={blocked}
          readOnly={Boolean(selectedSession?.readOnly)}
          disabledReason={disabledReason}
          harness={harness}
          status={status}
        />
      </section>
    </main>
  );
}
