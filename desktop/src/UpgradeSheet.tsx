import { useEffect, useRef, useState } from "react";
import {
  failure,
  request,
  type Failure,
  type PinOffer,
  type SetupPlan,
  type SetupResult,
} from "./api";

export type UpgradeMode =
  | { kind: "advance"; offer: PinOffer }
  | { kind: "refresh"; plan: SetupPlan };
type Step = "install" | "files" | "restart";
type State =
  | { kind: "loading" }
  | { kind: "plan-error"; problem: Failure }
  | { kind: "review"; plan: SetupPlan }
  | { kind: "running"; step: Step }
  | { kind: "failed"; step: Step; problem: Failure }
  | { kind: "complete"; result: SetupResult };

export function UpgradeSheet({
  repository,
  worktree,
  name,
  mode,
  onPinned,
  onRestart,
  onApplied,
  onClose,
}: {
  repository: string;
  worktree: string;
  name: string;
  mode: UpgradeMode;
  onPinned: () => Promise<void>;
  onRestart: () => Promise<void>;
  onApplied: () => void;
  onClose: () => void;
}) {
  const [state, setState] = useState<State>(() =>
    mode.kind === "refresh" ? { kind: "review", plan: mode.plan } : { kind: "loading" },
  );
  const [copyError, setCopyError] = useState("");
  const [copied, setCopied] = useState(false);
  const pinned = useRef(false);
  const result = useRef<SetupResult | null>(null);
  const running = useRef(false);
  const alive = useRef(true);
  const advance = mode.kind === "advance";
  const version = mode.kind === "advance" ? mode.offer.available : "";
  const title = advance
    ? `Upgrade ${name} to Rhizome ${version}`
    : `Update Rhizome files for ${name}`;
  const steps: { id: Step; label: string }[] = [
    ...(advance
      ? [{ id: "install" as const, label: `Install Rhizome ${version} and update the pin` }]
      : []),
    { id: "files", label: "Update Rhizome files" },
    ...(advance ? [{ id: "restart" as const, label: "Restart this worktree's runtime" }] : []),
  ];

  async function load() {
    setState({ kind: "loading" });
    try {
      const plan = await request("upgrade-plan", { id: repository, worktree });
      if (alive.current) setState({ kind: "review", plan });
    } catch (cause) {
      if (alive.current) setState({ kind: "plan-error", problem: failure(cause) });
    }
  }

  useEffect(() => {
    alive.current = true;
    if (advance) void load();
    return () => {
      alive.current = false;
    };
  }, []);

  async function run(from: Step) {
    if (running.current) return;
    running.current = true;
    let step = from;
    try {
      for (const next of steps.slice(steps.findIndex((s) => s.id === from))) {
        step = next.id;
        setState({ kind: "running", step });
        if (step === "install") {
          if (!pinned.current) {
            await request("pin-advance", { id: repository, worktree });
            pinned.current = true;
          }
          await onPinned();
        } else if (step === "files") {
          result.current = await request("settings-apply", {
            id: repository,
            worktree,
            choices: {},
          });
          onApplied();
        } else {
          await onRestart();
        }
      }
      if (result.current) setState({ kind: "complete", result: result.current });
    } catch (cause) {
      setState({ kind: "failed", step, problem: failure(cause) });
    } finally {
      running.current = false;
    }
  }

  async function copyCommit(done: SetupResult) {
    const paths = done.commit.length ? done.commit : [".rhizome/config.yml"];
    const quote = (path: string) => `'${path.replaceAll("'", "'\\''")}'`;
    const message = advance
      ? `chore: upgrade Rhizome to ${version}`
      : "chore: update Rhizome files";
    try {
      await navigator.clipboard.writeText(
        `git add -- ${paths.map(quote).join(" ")} && git commit -m "${message}"`,
      );
      setCopyError("");
      setCopied(true);
    } catch (cause) {
      setCopyError(failure(cause).message);
    }
  }

  const kept =
    state.kind === "review"
      ? (state.plan.writes.kept ?? [])
      : state.kind === "complete"
        ? state.result.kept
        : [];
  const changed =
    state.kind === "complete"
      ? [...new Set([...state.result.created, ...state.result.updated])]
      : [];
  return (
    <section className="setup-sheet upgrade-sheet" aria-labelledby="upgrade-title">
      <h1 id="upgrade-title">
        {state.kind === "complete"
          ? advance
            ? `Upgraded to ${version}`
            : "Rhizome files updated"
          : title}
      </h1>
      <p className="hint">
        <span className="path">{worktree}</span> · Changes stay uncommitted.
      </p>
      {state.kind === "loading" && <p role="status">Checking the upgrade…</p>}
      {state.kind === "plan-error" && (
        <div role="alert">
          <p className="error-text">{state.problem.message}</p>
          <button onClick={() => void load()}>Retry</button>
        </div>
      )}
      {state.kind === "review" && (
        <>
          {mode.kind === "advance" && (
            <div className="upgrade-group">
              <h2>Version</h2>
              <p>
                <span className="path">.rhizome/config.yml</span> · {mode.offer.pinned} → {version}
              </p>
              {mode.offer.caution && <p className="hint">{mode.offer.caution}</p>}
            </div>
          )}
          {(state.plan.writes.summary.length > 0 || state.plan.writes.files.length > 0) && (
            <div className="upgrade-group writes">
              <h2>Changes</h2>
              <ul className="writes-summary">
                {state.plan.writes.summary.map((line) => (
                  <li key={line}>{line}</li>
                ))}
              </ul>
              {state.plan.writes.files.length > 0 && (
                <details>
                  <summary>Show {state.plan.writes.files.length} files</summary>
                  <ul className="file-list">
                    {state.plan.writes.files.map((file) => (
                      <li key={file}>{file}</li>
                    ))}
                  </ul>
                </details>
              )}
            </div>
          )}
        </>
      )}
      {(state.kind === "running" || state.kind === "failed") && (
        <>
          <ol className="upgrade-steps" aria-label="Upgrade progress" aria-live="polite">
            {steps.map((step, index) => {
              const current = steps.findIndex((s) => s.id === state.step);
              const status =
                index < current
                  ? "done"
                  : index > current
                    ? "pending"
                    : state.kind === "failed"
                      ? "failed"
                      : "active";
              return (
                <li key={step.id} data-state={status}>
                  <span
                    className={status === "active" ? "spinner" : "upgrade-mark"}
                    role="img"
                    aria-label={status}
                  >
                    {status === "active"
                      ? null
                      : status === "done"
                        ? "✓"
                        : status === "failed"
                          ? "!"
                          : "○"}
                  </span>
                  {step.label}
                </li>
              );
            })}
          </ol>
          {state.kind === "failed" && (
            <p className="error-text" role="alert">
              {state.problem.message}
            </p>
          )}
        </>
      )}
      {state.kind === "complete" && (
        <>
          <div className="upgrade-group">
            <h2>
              {changed.length} {changed.length === 1 ? "file" : "files"} changed
            </h2>
            <ul className="file-list">
              {changed.map((file) => (
                <li key={file}>{file}</li>
              ))}
            </ul>
          </div>
          {state.result.warnings.length > 0 && (
            <div className="upgrade-group">
              <h2>Warnings</h2>
              <ul className="writes-summary warning-text">
                {state.result.warnings.map((line) => (
                  <li key={line}>{line}</li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
      {kept.length > 0 && (
        <div className="upgrade-group">
          <h2>Kept edited files</h2>
          <ul className="file-list">
            {kept.map((file) => (
              <li key={file}>{file}</li>
            ))}
          </ul>
          <p className="hint">
            Run <code>rzm init</code> in a terminal to compare and choose their updates.
          </p>
        </div>
      )}
      {copyError && (
        <p className="error-text" role="alert">
          Could not copy the command. {copyError}
        </p>
      )}
      <div className="setup-actions">
        <span className="hint">Teammates get these after you commit.</span>
        <div className="actions">
          {state.kind === "complete" ? (
            <>
              <button
                disabled={!advance && state.result.commit.length === 0}
                onClick={() => void copyCommit(state.result)}
              >
                {copied ? "Copied" : "Copy commit command"}
              </button>
              <button className="primary" onClick={onClose}>
                Done
              </button>
            </>
          ) : (
            <>
              <button disabled={state.kind === "running"} onClick={onClose}>
                Cancel
              </button>
              {state.kind === "review" && (
                <button className="primary" onClick={() => void run(advance ? "install" : "files")}>
                  {advance ? "Upgrade" : "Update files"}
                </button>
              )}
              {state.kind === "failed" && (
                <button className="primary" onClick={() => void run(state.step)}>
                  Retry
                </button>
              )}
            </>
          )}
        </div>
      </div>
    </section>
  );
}
