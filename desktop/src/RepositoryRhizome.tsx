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
  unsupported,
  onReload,
  disabled,
  onChanging,
}: RepositoryRhizomeProps & {
  unsupported: boolean;
  onReload: () => Promise<void>;
  disabled: boolean;
  onChanging?: (changing: boolean) => void;
}) {
  const [busy, setBusy] = useState(false);
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
    } finally {
      setBusy(false);
      onChanging?.(false);
    }
  }

  const useApp = () => appPath && void change(() => onSelect(appPath));
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
        {version && <p className="hint">Runs Rhizome {version}.</p>}
        {worktree.trusted && <p className="hint">Trusted on this Mac</p>}
        {offer && !unsupported && (
          <div className="actions start">
            <button disabled={busy || disabled} onClick={() => void change(onAdvance)}>
              Update {name} to {offer.available}
            </button>
            <span className="hint">
              Commit .rhizome/config.yml so teammates get the same version.
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
                onClick={() => void change(onAdvance)}
              >
                Update {name} to {offer.available}
              </button>
            )}
            {appPath && !development && (
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
          {(offer || (appPath && !development)) && (
            <p className="hint">
              {offer
                ? "Updating changes .rhizome/config.yml. Commit it so teammates get the same version."
                : managed
                  ? "Only this Mac changes. The repository stays pinned for everyone else. You can switch back under Rhizome version."
                  : "Only this Mac changes. You can switch back under Rhizome version."}
            </p>
          )}
          <p className="hint">
            Run <code>rzm init</code> in this folder from a terminal.
          </p>
        </div>
      )}
    </>
  );
}
