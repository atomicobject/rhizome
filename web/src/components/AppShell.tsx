import { useQuery, useQueryClient } from "@tanstack/react-query";
import { lazy, Suspense, useEffect, useRef, useState } from "react";

import { getStatus } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import type { StatusResponse } from "../api/types";
import { AgentWorkspace } from "./AgentWorkspace";
import { BareHTMLNote } from "./BareHTMLNote";
import { ErrorBoundary } from "./ErrorBoundary";
import { ExplorerWorkspace } from "./ExplorerWorkspace";
import { IndexReadyBanner } from "./IndexReadyBanner";
import { KeyboardShortcuts } from "./KeyboardShortcuts";
import { notifyLocationChange, useLocationSnapshot } from "./locationStore";
import { NotesShell } from "./NotesShell";
import { TypeLabelsProvider } from "./typeLabels";
import { NOTES_ROOT_PATH, parseNotesLocation } from "./notesRoute";
import { OntologyAtlasWorkspace } from "./OntologyAtlasWorkspace";
import { isOntologyPath, ONTOLOGY_ROOT_PATH } from "./ontologyRoute";
import { OntologyEditSessionProvider } from "./useOntologyEditSession";
import { useOntologySummaryQuery, useValidationQuery } from "./useNotesQueries";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";
import { validationHealth } from "./validation/validationPresentation";

type Route = "agent" | "explorer" | "graphql" | "notes" | "ontology";

const GraphQLExplorer = lazy(() => import("./GraphQLExplorer"));

function isExplorerPath(pathname: string): boolean {
  return pathname === "/explorer" || pathname.startsWith("/explorer/");
}

function isNotesPath(pathname: string): boolean {
  return pathname === NOTES_ROOT_PATH || pathname.startsWith(`${NOTES_ROOT_PATH}/`);
}

function isAgentPath(pathname: string): boolean {
  return pathname === "/agent" || pathname.startsWith("/agent/");
}

function isGraphQLPath(pathname: string): boolean {
  return pathname === "/graphql" || pathname.startsWith("/graphql/");
}

function normalizeRouteLocation(): Route {
  const { pathname } = window.location;

  if (pathname === "/") {
    window.history.replaceState({}, "", NOTES_ROOT_PATH);

    return "notes";
  }

  if (isExplorerPath(pathname)) return "explorer";

  if (isAgentPath(pathname)) return "agent";

  if (isGraphQLPath(pathname)) return "graphql";

  if (isOntologyPath(pathname)) return "ontology";

  if (isNotesPath(pathname)) return "notes";
  window.history.replaceState({}, "", NOTES_ROOT_PATH);

  return "notes";
}

function navigateTo(next: Route) {
  const url =
    next === "notes"
      ? NOTES_ROOT_PATH
      : next === "ontology"
        ? ONTOLOGY_ROOT_PATH
        : next === "agent"
          ? "/agent"
          : next === "graphql"
            ? "/graphql"
            : "/explorer";

  window.history.pushState({}, "", url);
  notifyLocationChange();
}

function GlobalSearch({ route }: { route: Route }) {
  const snapshot = useLocationSnapshot();
  const inputRef = useRef<HTMLInputElement>(null);
  const [value, setValue] = useState("");

  useEffect(() => {
    if (route !== "notes") {
      setValue("");

      return;
    }

    setValue(new URLSearchParams(snapshot.search).get("search") || "");
  }, [route, snapshot.search]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        inputRef.current?.focus();
        inputRef.current?.select();
      }
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const submit = () => {
    const query = value.trim();

    if (!query) return;
    const params = new URLSearchParams(route === "notes" ? snapshot.search : "");
    params.delete("note");
    params.delete("scope");
    params.delete("noteType");
    params.delete("folder");
    params.delete("searchTab");
    params.set("search", query);
    const pathname = route === "notes" ? snapshot.pathname : NOTES_ROOT_PATH;
    window.history.pushState({}, "", `${pathname}?${params.toString()}`);
    notifyLocationChange();
  };

  return (
    <form
      className="app-shell__global-search"
      role="search"
      aria-label="Project search"
      onSubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <input
        ref={inputRef}
        type="search"
        aria-label="Search this project"
        placeholder="Search this project…"
        value={value}
        onChange={(event) => setValue(event.target.value)}
      />
      <kbd>⌘K</kbd>
    </form>
  );
}

function NotesNavTools() {
  const summaryQuery = useOntologySummaryQuery();
  const validationQuery = useValidationQuery();
  const summary = summaryQuery.data;
  const issueCount = validationQuery.data?.snapshot?.issueCount;
  const health = validationHealth(validationQuery.data ?? null);

  return (
    <div className="app-shell__notes-tools">
      {/* Unknown counts stay hidden rather than reading as zero, as while the
          first index after a start is still rebuilding. */}
      {summary && !(summary.rebuilding && summary.totalNotes === 0) && (
        <>
          <span>
            <b>{summary.typedNotes}</b> typed
          </span>
          <span>
            <b>{summary.types?.length ?? 0}</b> types
          </span>
        </>
      )}
      <ValidationIssueBadge
        count={issueCount}
        health={health}
        label={issueCount ? `Open ${issueCount} validation issues` : "Open validation status"}
        onClick={() => {
          window.history.pushState({}, "", "/notes/issues");
          notifyLocationChange();
        }}
      />
    </div>
  );
}

export function AppShell() {
  const snapshot = useLocationSnapshot();
  const route = normalizeRouteLocation();

  // `?bare=1` on a note route renders the HTML note without workspace chrome;
  // "Open in browser tab" targets it.
  const bareNote =
    route === "notes" && new URLSearchParams(snapshot.search).get("bare") === "1"
      ? parseNotesLocation(snapshot.pathname, snapshot.search, snapshot.hash)
      : null;

  const [notesVisited, setNotesVisited] = useState(route === "notes");

  useEffect(() => {
    if (route === "notes") setNotesVisited(true);
  }, [route]);

  // WHY: one status owner at AppShell scope per SPEC-0071.US2.AC3 — do not
  // push a second read into ExplorerWorkspace. The query may refresh after a
  // reconnect/invalidation, while tab switches reuse the same cached value.
  const queryClient = useQueryClient();

  const statusQuery = useQuery({
    queryKey: queryKeys.status(),
    queryFn: getStatus,
  });

  const status = statusQuery.data ?? null;

  const setStatus = (next: StatusResponse | null) =>
    queryClient.setQueryData(queryKeys.status(), next);

  const connectionError = statusQuery.isError
    ? "Rhizome could not connect to the local workspace service."
    : null;

  const vaultName = status?.vaultName || null;

  useEffect(() => {
    if (vaultName) {
      document.title = `Rhizome · ${vaultName}`;
    }
  }, [vaultName]);

  if (bareNote?.note) {
    return (
      <BareHTMLNote path={bareNote.note} query={bareNote.query} fragment={bareNote.fragment} />
    );
  }

  return (
    <div className="app-shell">
      <header className="app-shell__nav">
        <div className="app-shell__brand">
          <div>
            <span>Rhizome</span>
            <small>{vaultName ?? (statusQuery.isError ? "Status unavailable" : "")}</small>
          </div>
        </div>
        <nav aria-label="Primary">
          <button
            type="button"
            className={route === "notes" ? "is-active" : ""}
            aria-current={route === "notes" ? "page" : undefined}
            onClick={() => navigateTo("notes")}
          >
            Notes
          </button>
          <button
            type="button"
            className={route === "ontology" ? "is-active" : ""}
            aria-current={route === "ontology" ? "page" : undefined}
            onClick={() => navigateTo("ontology")}
          >
            Ontology
          </button>
          <button
            type="button"
            className={route === "explorer" ? "is-active" : ""}
            aria-current={route === "explorer" ? "page" : undefined}
            onClick={() => navigateTo("explorer")}
          >
            Explorer
          </button>
          <button
            type="button"
            className={route === "agent" ? "is-active" : ""}
            aria-current={route === "agent" ? "page" : undefined}
            onClick={() => navigateTo("agent")}
          >
            Agent
          </button>
          <button
            type="button"
            className={route === "graphql" ? "is-active" : ""}
            aria-current={route === "graphql" ? "page" : undefined}
            onClick={() => navigateTo("graphql")}
          >
            GraphQL
          </button>
        </nav>
        <div className="app-shell__header-tools">
          <GlobalSearch route={route} />
          {route === "notes" ? <NotesNavTools /> : null}
          <KeyboardShortcuts />
        </div>
      </header>
      {connectionError && (
        <div className="app-shell__connection" role="alert">
          <div>
            <strong>Workspace unavailable</strong>
            <span>{connectionError} Data shown below may be incomplete.</span>
          </div>
          <button type="button" onClick={() => void statusQuery.refetch()}>
            Retry connection
          </button>
        </div>
      )}
      <IndexReadyBanner status={status} onStatusChange={setStatus} />
      <OntologyEditSessionProvider vaultKey={status?.vaultPath || null}>
        <ErrorBoundary resetKey={route} label="Rhizome could not render this view">
          {notesVisited || route === "notes" ? (
            <div
              className="app-shell__route-surface"
              hidden={route !== "notes"}
              inert={route === "notes" ? undefined : true}
            >
              <TypeLabelsProvider>
                <NotesShell active={route === "notes"} />
              </TypeLabelsProvider>
            </div>
          ) : null}
          {route === "notes" ? null : route === "ontology" ? (
            <OntologyAtlasWorkspace />
          ) : route === "agent" ? (
            <AgentWorkspace />
          ) : route === "graphql" ? (
            <Suspense fallback={<main className="graphql-explorer" />}>
              <GraphQLExplorer />
            </Suspense>
          ) : (
            <ExplorerWorkspace vaultName={vaultName ?? ""} />
          )}
        </ErrorBoundary>
      </OntologyEditSessionProvider>
    </div>
  );
}
