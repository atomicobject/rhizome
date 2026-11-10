import { useMutation, useQuery } from "@tanstack/react-query";
import { type ReactNode, useEffect, useState } from "react";
import type { components } from "../api/generated";
import { fetchJSON } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import { subscribeVaultEvents } from "../query/vaultEvents";
import { DisclosureChevron } from "./DisclosureChevron";
import { inDesktop, requestDesktop } from "./desktopHost";
import { StatusIcon, type StatusTone } from "./StatusMark";
import "./starterReadiness.css";

type Recovery = components["schemas"]["StarterRecovery"];

export type StarterState = components["schemas"]["StarterReadiness"];

const key = [...queryKeys.all, "starter-readiness"];

const states: Record<StarterState["state"], { label: string; tone: StatusTone }> = {
  checking: { label: "Checking setup…", tone: "progress" },
  working: { label: "Setting up…", tone: "progress" },
  required_input: { label: "Setup needs your input", tone: "neutral" },
  blocked: { label: "Setup blocked", tone: "risk" },
  failed: { label: "Setup failed", tone: "risk" },
  ready: { label: "Ready", tone: "success" },
};

function find<K extends Recovery["kind"]>(item: StarterState, kind: K) {
  return item.recovery?.find((action): action is Recovery & { kind: K } => action.kind === kind);
}

/**
 * Raw reasons and file lists open as a small popover, so the notice stays one
 * line. Escape closes it and returns focus to its summary.
 */
function Details({ summary, children }: { summary: string; children: ReactNode }) {
  return (
    <details
      className="starter-readiness__details"
      onKeyDown={(event) => {
        if (event.key !== "Escape" || !event.currentTarget.open) return;
        event.currentTarget.open = false;
        event.currentTarget.querySelector("summary")?.focus();
      }}
    >
      <summary className="button button--ghost">
        {summary} <DisclosureChevron />
      </summary>
      <div className="starter-readiness__popover">{children}</div>
    </details>
  );
}

function StarterRow({ item }: { item: StarterState }) {
  const [value, setValue] = useState("");

  const mutation = useMutation({
    mutationFn: (request: { kind: "retry" } | { kind: "choice"; value: string }) =>
      fetchJSON(`/api/v1/starter-readiness/${encodeURIComponent(item.module)}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(request),
      }),
  });

  const desktop = inDesktop();
  const state = states[item.state];
  const title = item.title || "Starter";
  const trust = find(item, "trust");
  const install = find(item, "install");
  const review = find(item, "review");
  const choice = find(item, "choice");
  // Trust is part of readiness, so trusting the repository resumes setup on its own.
  const retry = trust ? undefined : find(item, "retry");
  const settings = desktop ? (trust ?? install) : undefined;

  // One plain sentence stays visible. A problem keeps its full reason in
  // Details too, since a long reason is cut at two lines.
  const firstLine = item.reason?.split("\n")[0];

  const sentence: ReactNode = trust ? (
    desktop ? (
      "This repository isn't trusted yet."
    ) : (
      <>
        Setup continues after you run <code>rzm trust</code> in this repository.
      </>
    )
  ) : item.state === "failed" ? null : (
    firstLine
  );

  const problem = item.state === "blocked" || item.state === "failed";

  const hint =
    install && !desktop
      ? "Run rzm init in this repository to review its installation."
      : review
        ? "Resolve the reported conflict in these files, then retry setup."
        : undefined;

  const pending = mutation.isPending;

  return (
    <div className="starter-readiness__row" role="group" aria-label={`${title} setup`}>
      <StatusIcon tone={state.tone} />
      <p className="starter-readiness__text" role="status">
        <span className="starter-readiness__subject">{title}</span>
        <span className="starter-readiness__state"> · {state.label}</span>
        {sentence ? <span className="starter-readiness__sentence"> {sentence}</span> : null}
      </p>
      <div className="starter-readiness__actions">
        {choice ? (
          <select
            aria-label={choice.label}
            value={value}
            disabled={pending}
            onChange={(event) => setValue(event.target.value)}
          >
            <option value="">Choose a product</option>
            {choice.choices.map((option) => (
              <option key={option}>{option}</option>
            ))}
          </select>
        ) : null}
        {(item.reason && (problem || item.reason !== firstLine)) || review || hint ? (
          <Details summary={review?.label ?? "Details"}>
            {item.reason ? <p className="starter-readiness__reason">{item.reason}</p> : null}
            {review?.paths?.length ? (
              <ul className="starter-readiness__paths" aria-label="Files">
                {review.paths.map((path) => (
                  <li key={path}>
                    <code>{path}</code>
                  </li>
                ))}
              </ul>
            ) : null}
            {hint ? <p className="starter-readiness__hint">{hint}</p> : null}
          </Details>
        ) : null}
        {retry ? (
          <button
            type="button"
            className={settings ? "button button--ghost" : "button"}
            disabled={pending}
            onClick={() => mutation.mutate({ kind: "retry" })}
          >
            {pending ? "Retrying…" : retry.label}
          </button>
        ) : null}
        {choice ? (
          <button
            type="button"
            className="button"
            disabled={pending || !value}
            onClick={() => mutation.mutate({ kind: "choice", value })}
          >
            {pending ? "Applying…" : "Use this product"}
          </button>
        ) : null}
        {settings ? (
          <button
            type="button"
            className="button"
            onClick={() => requestDesktop({ action: "openRepositorySettings" })}
          >
            {settings.label}
          </button>
        ) : null}
      </div>
      {mutation.error ? (
        <p className="starter-readiness__error" role="alert">
          {mutation.error.message}
        </p>
      ) : null}
    </div>
  );
}

export function StarterReadiness() {
  const query = useQuery({
    queryKey: key,
    queryFn: () => fetchJSON<StarterState[]>("/api/v1/starter-readiness"),
    refetchInterval: 5000,
  });

  useEffect(
    () =>
      subscribeVaultEvents(({ event }) => {
        if (event === "starter-readiness.changed" || event === "stream.reconnected")
          void query.refetch();
      }),
    [query.refetch],
  );

  if (query.isError)
    return (
      <aside className="starter-readiness" aria-label="Starter setup">
        <div className="starter-readiness__card">
          <div className="starter-readiness__row" role="alert">
            <StatusIcon tone="warning" />
            <p className="starter-readiness__text">
              <span className="starter-readiness__subject">Setup status unavailable</span>
              <span className="starter-readiness__sentence">
                {" "}
                Checking again every few seconds.
              </span>
            </p>
            <div className="starter-readiness__actions">
              <Details summary="Details">
                <p className="starter-readiness__reason">{query.error.message}</p>
              </Details>
              <button type="button" className="button" onClick={() => void query.refetch()}>
                Retry now
              </button>
            </div>
          </div>
        </div>
      </aside>
    );

  const items = query.data?.filter((item) => item.state !== "ready") ?? [];

  if (!items.length) return null;

  return (
    <aside className="starter-readiness" aria-label="Starter setup">
      <div className="starter-readiness__card">
        {items.map((item) => (
          <StarterRow key={item.module} item={item} />
        ))}
      </div>
    </aside>
  );
}
