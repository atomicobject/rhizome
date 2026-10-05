import { useEffect, useState } from "react";
import type { GlobalInfo } from "./api";
import { version } from "../package.json";
export function Settings({
  info,
  selected,
  busy,
  onRefresh,
  onInstall,
  onUpdate,
  onSelect,
  onPick,
  onClose,
  developmentBuilds = [],
}: {
  info: GlobalInfo | null;
  selected?: string;
  busy: boolean;
  onRefresh: () => void;
  onInstall: () => void;
  onUpdate: () => void;
  onSelect: (value: string | null) => void;
  onPick: () => void;
  onClose: () => void;
  developmentBuilds?: { repository: string; worktree: string; executable: string }[];
}) {
  const [path, setPath] = useState(selected ?? "");
  useEffect(() => setPath(selected ?? ""), [selected]);
  return (
    <section className="settings" aria-labelledby="installation-title">
      <div className="section-heading">
        <div>
          <h1 id="installation-title">Rhizome installation</h1>
          <p>The command line runtime used by your repositories.</p>
        </div>
        <div className="actions">
          <button disabled={busy} onClick={onRefresh}>
            Refresh
          </button>
          <button onClick={onClose}>Done</button>
        </div>
      </div>
      <div className="installation-status">
        <div>
          <h2>
            {info?.installed
              ? `Rhizome ${info.version || "(version unavailable)"}`
              : info
                ? "Rhizome is not installed"
                : "Checking installation…"}
          </h2>
          <p className="path">{info?.path || info?.managedPath}</p>
        </div>
        {info?.installed && <span className="badge">Installed</span>}
      </div>
      {developmentBuilds.length > 0 && (
        <div className="development-builds">
          <h2>Developer builds</h2>
          <p>
            These worktrees run a local build instead of the global installation. When a build
            changes, its running worktrees restart on it.
          </p>
          <ul>
            {developmentBuilds.map((build) => (
              <li key={`${build.repository}\0${build.worktree}`}>
                <span>
                  <strong>{build.repository}</strong>{" "}
                  <span className="branch">{build.worktree}</span>
                </span>
                <span className="path">{build.executable}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      <p>
        Each worktree keeps its configured version. The global installation is used when a worktree
        does not select its own runtime.
      </p>
      {info && (
        <div className="install-actions">
          {info.canUpdate ? (
            <button className="primary" disabled={busy} onClick={onUpdate}>
              Update global Rhizome
            </button>
          ) : (
            <button className="primary" disabled={busy} onClick={onInstall}>
              Install managed Rhizome
            </button>
          )}
          <p className="hint">
            {info.canUpdate ? "Updates" : "Installs to"}{" "}
            <span className="path">{info.managedPath}</span>
            {info.installed && !info.canUpdate
              ? ". Your current installation is managed elsewhere."
              : "."}
          </p>
        </div>
      )}
      <div className="setting-group">
        <h2>Use an existing executable</h2>
        <p>Choose an installed or locally built Rhizome binary.</p>
        <label htmlFor="global-executable">Global executable path</label>
        <div className="input-actions">
          <input
            id="global-executable"
            className="path"
            value={path}
            onChange={(e) => setPath(e.target.value)}
            placeholder="/absolute/path/to/rzm"
          />
          <button disabled={busy} onClick={onPick}>
            Browse…
          </button>
          <button disabled={busy || !path.trim()} onClick={() => onSelect(path.trim())}>
            Use executable
          </button>
        </div>
        {selected && (
          <p className="hint">
            Selected: <span className="path">{selected}</span>{" "}
            <button
              className="text-button"
              disabled={busy}
              onClick={() => {
                setPath("");
                onSelect(null);
              }}
            >
              Use automatic discovery
            </button>
          </p>
        )}
      </div>
      <footer className="settings-footer">
        Rhizome Desktop {version}
        <span>The desktop app and Rhizome runtime update separately.</span>
      </footer>
    </section>
  );
}
