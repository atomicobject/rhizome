import { useMutation, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import type { components } from "../api/generated";
import { fetchJSON } from "../api/client";
import { queryKeys } from "../api/queryKeys";
import { subscribeVaultEvents } from "../query/vaultEvents";
import { inDesktop, requestDesktop } from "./desktopHost";
import "./starterReadiness.css";

type Recovery = components["schemas"]["StarterRecovery"];

export type StarterState = components["schemas"]["StarterReadiness"];

const key = [...queryKeys.all, "starter-readiness"];

const labels = {
  checking: "Checking setup",
  working: "Setting up",
  required_input: "Setup needs your input",
  blocked: "Setup blocked",
  failed: "Setup failed",
  ready: "Ready",
};

function RecoveryControl({ module, action }: { module: string; action: Recovery }) {
  const [value, setValue] = useState("");

  const mutation = useMutation({
    mutationFn: (request: { kind: "retry" } | { kind: "choice"; value: string }) =>
      fetchJSON(`/api/v1/starter-readiness/${encodeURIComponent(module)}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(request),
      }),
  });

  if (action.kind === "review")
    return (
      <details>
        <summary>{action.label}</summary>
        {action.paths?.map((path) => (
          <code key={path}>{path}</code>
        ))}
        <p>Resolve the reported conflict in these files, then retry setup.</p>
      </details>
    );

  if (action.kind === "trust" || action.kind === "install")
    return inDesktop() ? (
      <button type="button" onClick={() => requestDesktop({ action: "openRepositorySettings" })}>
        {action.label}
      </button>
    ) : (
      <p>
        {action.kind === "trust"
          ? "Run rzm trust in this repository, then retry."
          : "Run rzm init in this repository to review its installation."}
      </p>
    );

  return (
    <div>
      {action.kind === "choice" ? (
        <label>
          {action.label}
          <select value={value} onChange={(event) => setValue(event.target.value)}>
            <option value="">Choose a product</option>
            {action.choices.map((choice) => (
              <option key={choice}>{choice}</option>
            ))}
          </select>
        </label>
      ) : null}
      <button
        type="button"
        disabled={mutation.isPending || (action.kind === "choice" && !value)}
        onClick={() =>
          mutation.mutate(action.kind === "choice" ? { kind: "choice", value } : { kind: "retry" })
        }
      >
        {mutation.isPending
          ? "Requested…"
          : action.kind === "choice"
            ? "Use this product"
            : action.label}
      </button>
      {mutation.error ? <p role="alert">{mutation.error.message}</p> : null}
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
      <aside className="starter-readiness" role="alert">
        Starter setup status is unavailable.{" "}
        <button type="button" onClick={() => void query.refetch()}>
          Retry connection
        </button>
      </aside>
    );

  return (
    <>
      {query.data
        ?.filter((item) => item.state !== "ready")
        .map((item) => (
          <aside className="starter-readiness" key={item.module} aria-label={`${item.title} setup`}>
            <strong>
              {item.title} · {labels[item.state]}
            </strong>
            {item.reason ? <p role="status">{item.reason}</p> : null}
            {item.recovery?.map((action, index) => (
              <RecoveryControl
                key={`${item.module}-${index}-${action.kind}`}
                module={item.module}
                action={action}
              />
            ))}
          </aside>
        ))}
    </>
  );
}
