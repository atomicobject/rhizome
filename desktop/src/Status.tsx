import { useEffect, useState, type ReactNode } from "react";
import type { Failure, OpenState } from "./api";

const progress: Partial<Record<OpenState["step"], string>> = {
  checking: "Checking the worktree",
  seeding: "Copying the index",
  starting: "Starting Rhizome",
  loading: "Loading the workspace",
};

/** Progress shorter than this, such as reopening a running worktree, shows nothing. */
const QUIET_MS = 800;

/** Whether `delay` has passed since `started`, rerendering when it does. */
function useElapsedPast(started: number, delay: number) {
  const [past, setPast] = useState(() => Date.now() - started >= delay);
  useEffect(() => {
    const wait = started + delay - Date.now();
    setPast(wait <= 0);
    if (wait <= 0) return;
    const timer = window.setTimeout(() => setPast(true), wait);
    return () => window.clearTimeout(timer);
  }, [started, delay]);
  return past;
}

function Elapsed({ started }: { started: number }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  return <span className="elapsed">{Math.max(0, Math.floor((now - started) / 1000))}s</span>;
}

const reconnecting = {
  stopped: "Rhizome stopped — reconnecting",
  moved: "Rhizome moved to a new address — reconnecting",
};

function basename(path: string) {
  return path.split("/").filter(Boolean).pop() ?? path;
}

export function OpenStatus({
  state,
  busy,
  onRetry,
  onWake,
  onTrust,
  onSetup,
  onSkipSeed,
  onChooseExecutable,
  onManageInstallation,
}: {
  state: OpenState;
  busy: boolean;
  onRetry: () => void;
  onWake: () => void;
  onTrust: () => void;
  onSetup: () => void;
  onSkipSeed: () => void;
  onChooseExecutable: () => void;
  onManageInstallation: () => void;
}) {
  const title = progress[state.step];
  const shown = useElapsedPast(state.started, QUIET_MS);
  if (state.reconnecting && (title || state.step === "error")) {
    return (
      <div className="status-panel" role="status" aria-live="polite">
        <span className="spinner" aria-hidden="true" />
        <h1>
          {reconnecting[state.reconnecting]}… <Elapsed started={state.started} />
        </h1>
        {title && <p>{title}…</p>}
        {state.step === "error" && (
          <p className="error-text">The last attempt failed: {state.message}</p>
        )}
        <p className="path">{state.worktree}</p>
      </div>
    );
  }
  if (title) {
    if (!shown) return null;
    return (
      <div className="status-panel" role="status" aria-live="polite">
        <span className="spinner" aria-hidden="true" />
        <h1>
          {title}
          {state.step === "seeding" && state.primary
            ? ` from ${basename(state.primary)}`
            : ""}…{" "}
          <Elapsed started={state.started} />
        </h1>
        <p className="path">{state.worktree}</p>
        {state.step === "seeding" && (
          <p className="hint">
            The new worktree starts from the primary worktree’s index, then catches up.
          </p>
        )}
      </div>
    );
  }
  if (state.step === "sleeping") {
    return (
      <div className="status-panel">
        <h1>Rhizome is sleeping</h1>
        <p>You stopped Rhizome for this worktree. It starts again when you open it.</p>
        <p className="path">{state.worktree}</p>
        <button className="primary" disabled={busy} onClick={onWake}>
          Start Rhizome
        </button>
      </div>
    );
  }
  if (state.step === "trust") {
    return (
      <div className="status-panel">
        <h1>Trust this worktree?</h1>
        <p>
          Opening it can run the Rhizome executable this worktree selects and download its pinned
          version. Trust is recorded for this worktree only.
        </p>
        <p className="path confirmation-path">{state.worktree}</p>
        <button className="primary" disabled={busy} onClick={onTrust}>
          Trust and open
        </button>
      </div>
    );
  }
  if (state.step === "setup") {
    return (
      <div className="status-panel">
        <h1>Rhizome is not set up here</h1>
        <p>
          Setup creates Rhizome’s configuration and agent guidance in this worktree. Existing
          configuration is preserved. You can also run <code>rzm init</code> yourself.
        </p>
        <p className="path confirmation-path">{state.worktree}</p>
        <button className="primary" disabled={busy} onClick={onSetup}>
          Set up Rhizome
        </button>
      </div>
    );
  }
  if (state.step === "seed-error" || state.step === "error") {
    return (
      <div className="status-panel" role="alert">
        <h1>
          {state.step === "seed-error"
            ? "Could not copy the index"
            : "Could not open this worktree"}
        </h1>
        <p className="error-text">{state.message}</p>
        <p className="path">{state.worktree}</p>
        <div className="actions">
          <button className="primary" disabled={busy} onClick={onRetry}>
            Try again
          </button>
          {state.step === "seed-error" && (
            <button disabled={busy} onClick={onSkipSeed}>
              Start without copying
            </button>
          )}
          {state.code === "external_executable_required" && (
            <button disabled={busy} onClick={onChooseExecutable}>
              Choose executable…
            </button>
          )}
          {state.code === "missing_executable" && (
            <button disabled={busy} onClick={onManageInstallation}>
              Manage installation
            </button>
          )}
        </div>
      </div>
    );
  }
  return null;
}

export function Notice({
  title,
  failure,
  children,
}: {
  title: string;
  failure?: Failure | null;
  children?: ReactNode;
}) {
  return (
    <div className="status-panel" role={failure ? "alert" : undefined}>
      <h1>{title}</h1>
      {failure && <p className="error-text">{failure.message}</p>}
      {children}
    </div>
  );
}
