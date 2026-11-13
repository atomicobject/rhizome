import { useState } from "react";
import { failure, type Failure, type GlobalInfo, type Worktree } from "./api";

export interface RepositoryRhizomeProps {
  name: string;
  worktree: Worktree;
  chosen?: string;
  app: GlobalInfo | null;
  runningVersion?: string;
  onSelect: (executable: string | null) => Promise<void>;
  onChoose: () => Promise<boolean>;
  onAdvance: () => Promise<void>;
  onTrust?: () => Promise<void>;
}

/** The local execution choice is independent of the shared repository pin. */
export function RepositoryRhizome({
  name,
  worktree,
  chosen,
  app,
  runningVersion,
  onSelect,
  onChoose,
  onAdvance,
  onTrust,
  unsupported,
  onReload,
  disabled,
  note,
  onChanging,
}: RepositoryRhizomeProps & {
  unsupported: boolean;
  onReload: () => Promise<void>;
  disabled: boolean;
  /** Why the choice is locked, when it is. */
  note?: string;
  onChanging?: (changing: boolean) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [trusting, setTrusting] = useState(false);
  const [error, setError] = useState<Failure | null>(null);
  const managed = worktree.authority === "managed";
  const development = worktree.authority === "development";
  const pin = worktree.pinnedVersion;
  const appPath = app?.bundledPath || app?.path;
  const selection = chosen ? (chosen === appPath ? "app" : "other") : "default";
  const offer = managed && worktree.pinOffer?.state === "advance" ? worktree.pinOffer : null;
  const defaultVersion = managed ? pin : development ? undefined : app?.version;
  const selectedVersion = !chosen ? defaultVersion : chosen === app?.path ? app.version : undefined;
  const version = runningVersion || selectedVersion;

  async function change(action: () => Promise<void | boolean>) {
    setBusy(true);
    onChanging?.(true);
    setError(null);
    try {
      if ((await action()) !== false) await onReload();
    } catch (cause) {
      setError(failure(cause));
      // The choice may be saved even when the restart after it failed, so
      // settings read with the previous executable are stale either way.
      await onReload().catch(() => {});
    } finally {
      setBusy(false);
      onChanging?.(false);
    }
  }

  const useApp = () => appPath && void change(() => onSelect(appPath));

  async function trust() {
    if (!onTrust) return;
    setTrusting(true);
    setError(null);
    try {
      await onTrust();
      await onReload();
    } catch (cause) {
      setError(failure(cause));
    } finally {
      setTrusting(false);
    }
  }
  // Offered only when it would change what runs.
  const canUseApp = !!appPath && !development && selection !== "app";
  const unavailable = unsupported && (
    <span className="recommended warning-ink">Can't change settings from the app</span>
  );
  const defaultLabel = managed
    ? `Pinned version, ${pin || "unknown"}`
    : worktree.authority === "external"
      ? "External binary manager"
      : "Global Rhizome";

  return (
    <>
      <section className="repo-rhizome" aria-labelledby="repo-rhizome-title">
        <h2 id="repo-rhizome-title">Rhizome version</h2>
        {development ? (
          <p>
            Development source checkout {unavailable}
            <span className="path choice-detail">{worktree.executable}</span>
          </p>
        ) : (
          <fieldset className="setup-form" disabled={busy || disabled}>
            <div className="choices" role="radiogroup" aria-label="Rhizome executable">
              <label className="choice">
                <input
                  type="radio"
                  name="repo-executable"
                  checked={selection === "default"}
                  onChange={() => void change(() => onSelect(null))}
                />
                <span className="choice-label">
                  {defaultLabel} {selection === "default" && unavailable}
                </span>
                <span className="choice-detail">
                  {managed
                    ? "From .rhizome/config.yml. Everyone gets this version."
                    : worktree.authority === "external"
                      ? "Uses a running Rhizome or the app's global executable."
                      : app?.path || "No global executable found."}
                </span>
              </label>
              {appPath && (
                <label className="choice">
                  <input
                    type="radio"
                    name="repo-executable"
                    checked={selection === "app"}
                    onChange={useApp}
                  />
                  <span className="choice-label">
                    The app's Rhizome {selection === "app" && unavailable}
                  </span>
                  <span className="choice-detail">
                    {managed ? "This Mac only. The pin stays as it is." : "This Mac only."}
                  </span>
                </label>
              )}
              <label className="choice">
                <input
                  type="radio"
                  name="repo-executable"
                  checked={selection === "other"}
                  onChange={() => void change(onChoose)}
                  onClick={() => selection === "other" && void change(onChoose)}
                />
                <span className="choice-label">
                  Another executable… {selection === "other" && unavailable}
                </span>
                {chosen && selection === "other" && (
                  <span className="choice-detail path">{chosen}</span>
                )}
              </label>
            </div>
          </fieldset>
        )}
        {note && <p className="hint">{note}</p>}
        {version && <p className="hint">Runs Rhizome {version}.</p>}
        {worktree.trusted && <p className="hint">Trusted on this Mac</p>}
        {!worktree.trusted && onTrust && (
          <div className="actions start">
            <span className="hint">Not trusted on this Mac</span>
            <button disabled={busy || disabled || trusting} onClick={() => void trust()}>
              {trusting ? "Trusting…" : "Trust this worktree"}
            </button>
          </div>
        )}
        {offer && !unsupported && (
          <div className="actions start">
            <button disabled={busy || disabled} onClick={() => void onAdvance()}>
              Update {name} to {offer.available}
            </button>
            <span className="hint">
              Review the version and Rhizome file changes before upgrading.
            </span>
          </div>
        )}
        {error && (
          <p className="error-text" role="alert">
            {error.message}
          </p>
        )}
        {busy && <p role="status">Changing Rhizome…</p>}
      </section>
      {unsupported && (
        <div className="repo-settings-status" role="alert">
          <h2>This repository's Rhizome is too old to change settings here</h2>
          <p>
            {name} runs{" "}
            {version
              ? `Rhizome ${version}`
              : chosen || worktree.executable || "the selected Rhizome executable"}
            . This executable cannot report these settings.
          </p>
          <div className="actions start">
            {offer && (
              <button
                className="primary"
                disabled={busy || disabled}
                onClick={() => void onAdvance()}
              >
                Update {name} to {offer.available}
              </button>
            )}
            {canUseApp && (
              <button
                className={offer ? "" : "primary"}
                disabled={busy || disabled}
                onClick={useApp}
              >
                {offer
                  ? "Use the app's Rhizome instead"
                  : "Use the app's Rhizome for this repository"}
              </button>
            )}
          </div>
          {(offer || canUseApp) && (
            <p className="hint">
              {offer
                ? "Review the version and Rhizome file changes before upgrading."
                : managed
                  ? "Only this Mac changes. The repository stays pinned for everyone else. You can switch back under Rhizome version."
                  : "Only this Mac changes. You can switch back under Rhizome version."}
            </p>
          )}
          <p className="hint">
            {offer || canUseApp ? "Or run" : "Run"} <code>rzm init</code> in this folder from a
            terminal.
          </p>
        </div>
      )}
    </>
  );
}
