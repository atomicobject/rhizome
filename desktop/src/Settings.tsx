import { useEffect, useState, type CSSProperties } from "react";
import { THEMES } from "../../web/src/lib/themes";
import { CredentialImport } from "./CredentialImport";
import {
  failure,
  request,
  type AppearanceChoice,
  type AppSettings,
  type GlobalInfo,
  type LinkInfo,
  type RenderProfiling,
  type UpdateStatus,
} from "./api";

const linkText: Record<LinkInfo["state"], string> = {
  absent: "Not installed. Install it to run rzm from terminals and coding agents.",
  installed: "Installed. rzm runs this app's Rhizome and stops working when the app is deleted.",
  "other-bundle": "Points to another copy of Rhizome.",
  dangling: "Points to a Rhizome app that no longer exists.",
  foreign: "Another rzm is installed here. Rhizome leaves it alone.",
  unsupported: "Available in the released Rhizome app.",
};

const linkAction: Partial<Record<LinkInfo["state"], string>> = {
  absent: "Install Command-Line Tool",
  "other-bundle": "Use This App's rzm",
  dangling: "Repair Command-Line Tool",
};

function updateText(status: UpdateStatus | null): string {
  switch (status?.state) {
    case "current":
      return `Rhizome ${status.version} is up to date.`;
    case "checking":
      return "Checking for updates…";
    case "downloading":
      return `Downloading Rhizome ${status.version}…`;
    case "ready":
      return `Rhizome ${status.version} is ready. Restart to finish updating; open repositories reopen afterward.`;
    case "failed":
      return `Could not check for updates: ${status.message}`;
    case "disabled":
      return "This development build does not update itself. Rhizome Dev is updated with make desktop-install-dev.";
    default:
      return "Rhizome checks for updates at launch and once a day.";
  }
}

/** Homebrew's rzm comes before /usr/local/bin on most Apple silicon PATHs. */
function shadowHint(path: string): string {
  return /homebrew|Cellar/.test(path)
    ? " Run brew uninstall rhizome to use this app's rzm."
    : " Remove it or move it later in your PATH to use this app's rzm.";
}

/** React render regions for `rzm diagnostics perf` (SPEC-0116). */
function Diagnostics() {
  const [profiling, setProfiling] = useState<RenderProfiling | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    void request("render-profiling", {})
      .then(setProfiling)
      .catch((cause) => setError(failure(cause).message));
  }, []);
  async function toggle(enabled: boolean) {
    setSaving(true);
    setError("");
    try {
      setProfiling(await request("set-render-profiling", { enabled }));
    } catch (cause) {
      setError(failure(cause).message);
    } finally {
      setSaving(false);
    }
  }
  return (
    <div className="setting-group">
      <h2>Diagnostics</h2>
      <p>
        Render timings measure how often parts of the page re-render, for rzm diagnostics perf. They
        add noticeable overhead while on.
      </p>
      <p className="hint">
        {profiling?.always
          ? "Always on in Rhizome Dev."
          : "Applies to windows opened after you change it."}
      </p>
      <div className="inline-choices">
        <label>
          <input
            type="checkbox"
            checked={profiling?.enabled ?? false}
            disabled={!profiling || profiling.always || saving}
            onChange={(e) => void toggle(e.target.checked)}
          />
          Record render timings
        </label>
      </div>
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}

const APPEARANCES: [AppearanceChoice, string][] = [
  ["auto", "Match System"],
  ["light", "Light"],
  ["dark", "Dark"],
];

/** The app-wide appearance and chrome theme (SPEC-0126 US1). */
function Appearance({
  settings,
  onChange,
}: {
  settings: AppSettings;
  onChange: (change: Partial<AppSettings>) => void;
}) {
  return (
    <div className="setting-group">
      <h2>Appearance</h2>
      <p>Applies to every window and repository, including the pages they show.</p>
      <div className="segmented" role="radiogroup" aria-label="Appearance">
        {APPEARANCES.map(([appearance, label]) => (
          <label key={appearance}>
            <input
              type="radio"
              name="appearance"
              checked={settings.appearance === appearance}
              onChange={() => onChange({ appearance })}
            />
            {label}
          </label>
        ))}
      </div>
      <div className="theme-swatches" role="radiogroup" aria-label="Theme">
        {THEMES.map((theme) => (
          <label key={theme.id} style={{ "--swatch": theme.swatch } as CSSProperties}>
            <input
              type="radio"
              name="theme"
              checked={settings.theme === theme.id}
              onChange={() => onChange({ theme: theme.id })}
            />
            <span className="swatch" aria-hidden="true" />
            {theme.label}
          </label>
        ))}
      </div>
    </div>
  );
}

export function Settings({
  info,
  link,
  update,
  selected,
  busy,
  onRefresh,
  onLink,
  onCheckUpdates,
  onInstallUpdate,
  onSelect,
  onPick,
  onClose,
  appearance,
  onAppearance,
  developmentBuilds = [],
}: {
  info: GlobalInfo | null;
  link: LinkInfo | null;
  update: UpdateStatus | null;
  selected?: string;
  busy: boolean;
  onRefresh: () => void;
  onLink: (action: "install" | "remove") => void;
  onCheckUpdates: () => void;
  onInstallUpdate: () => void;
  onSelect: (value: string | null) => void;
  onPick: () => void;
  onClose: () => void;
  appearance: AppSettings;
  onAppearance: (change: Partial<AppSettings>) => void;
  developmentBuilds?: { repository: string; worktree: string; executable: string }[];
}) {
  const [path, setPath] = useState(selected ?? "");
  useEffect(() => setPath(selected ?? ""), [selected]);
  const action = link && linkAction[link.state];
  const shell = link?.shell;
  const updating = update?.state === "checking" || update?.state === "downloading";
  return (
    <section className="settings" aria-labelledby="installation-title">
      <div className="section-heading">
        <div>
          <h1 id="installation-title">Rhizome</h1>
          <p>The app, its command-line tool, and the runtime your repositories use.</p>
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
            {info
              ? info.path
                ? `Rhizome ${info.version || "(version unavailable)"}`
                : "No Rhizome executable found"
              : "Checking installation…"}
          </h2>
          <p className="path">{info?.path}</p>
        </div>
        {info?.path && <span className="badge">{info.bundled ? "Bundled" : "Selected"}</span>}
      </div>
      <Appearance settings={appearance} onChange={onAppearance} />
      <div className="setting-group">
        <h2>Updates</h2>
        <p role="status">{updateText(update)}</p>
        <div className="actions">
          {update?.state === "ready" ? (
            <button className="primary" onClick={onInstallUpdate}>
              Restart to Update
            </button>
          ) : (
            <button disabled={updating || update?.state === "disabled"} onClick={onCheckUpdates}>
              Check for Updates
            </button>
          )}
        </div>
      </div>
      <div className="setting-group">
        <h2>Command-line tool</h2>
        <p>
          {link ? linkText[link.state] : "Checking the command-line tool…"}
          {link?.target && link.state !== "installed" && (
            <>
              {" "}
              <span className="path">{link.target}</span>
            </>
          )}
        </p>
        {link && <p className="path">{link.linkPath}</p>}
        {shell?.error && <p className="hint">Could not check your login shell: {shell.error}</p>}
        {shell && !shell.error && (
          <p className="hint">
            {shell.path ? (
              <>
                Your login shell runs <span className="path">{shell.path}</span>
                {shell.version ? ` (${shell.version})` : ""}.
                {!shell.isApp && link?.state === "installed" && shadowHint(shell.path)}
              </>
            ) : (
              "Your login shell finds no rzm."
            )}
          </p>
        )}
        {link && (action || link.state === "installed") && (
          <div className="actions">
            {action ? (
              <button className="primary" disabled={busy} onClick={() => onLink("install")}>
                {action}
              </button>
            ) : (
              <button disabled={busy} onClick={() => onLink("remove")}>
                Remove Command-Line Tool
              </button>
            )}
          </div>
        )}
        {action && <p className="hint">macOS asks for an administrator password when needed.</p>}
      </div>
      <CredentialImport />
      <Diagnostics />
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
        Each repository keeps its pinned Rhizome version. The app's Rhizome sets up new repositories
        and runs worktrees that select no version of their own.
      </p>
      <div className="setting-group">
        <h2>Use an existing executable</h2>
        <p>Choose an installed or locally built Rhizome binary instead of the app's.</p>
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
              Use the app's Rhizome
            </button>
          </p>
        )}
      </div>
    </section>
  );
}
