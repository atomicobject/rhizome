import { useState } from "react";
import { failure, type ProviderReport, type Runtime } from "./api";

export const providerNames = { voyage: "Voyage", typesafe: "Jev" };

/** Reports describe provenance only; key values never cross this boundary. */
export function routeText(report: ProviderReport): string {
  switch (report.source) {
    case "team":
      return "Atomic Object server";
    case "direct":
      switch (report.key) {
        case "environment":
          return `${report.envName || "API key"} from the environment`;
        case "flag":
          return "Key supplied on the command line";
        case "saved":
          return "Your saved key";
        default:
          return "Your API key";
      }
    case "bundle":
      return "Legacy Atomic key bundle";
    case "none":
      if (report.reason === "configuration_unavailable")
        return "Unavailable: repair your Rhizome user configuration";
      if (report.reason === "invalid_preference")
        return "Unavailable: choose a valid provider source";
      if (report.reason === "session_unavailable")
        return "Unavailable: check your server session and unlock your keychain";
      if (report.reason === "session_expired") return "Unavailable: sign in again";
      return report.preference === "server"
        ? "Unavailable: sign in to the Atomic Object server"
        : report.preference === "key"
          ? "Unavailable: add your key below"
          : "Unavailable: sign in or add a key";
  }
}

export interface ProviderRuntime {
  repository: string;
  name: string;
  worktree: string;
  runtime: Runtime;
}

export function ProviderRuntimes({
  repositories,
  onRestart,
}: {
  repositories: ProviderRuntime[];
  onRestart: (repository: string, worktree: string) => Promise<void>;
}) {
  const [restarting, setRestarting] = useState<string[]>([]);
  const [error, setError] = useState("");
  async function restart(item: ProviderRuntime) {
    setRestarting((current) => [...current, item.worktree]);
    setError("");
    try {
      await onRestart(item.repository, item.worktree);
    } catch (cause) {
      setError(failure(cause).message);
    } finally {
      setRestarting((current) => current.filter((path) => path !== item.worktree));
    }
  }
  const running = repositories.filter((item) => item.runtime.state === "running");
  return (
    <div className="provider-subsection">
      <h3>Running repositories</h3>
      <p className="hint">
        These are the connections each runtime constructed, not a record of successful requests.
        After replacing a saved Voyage key, restart every repository using it.
      </p>
      {running.length === 0 ? (
        <p className="hint">
          No repositories are running. Open one to see the source it actually uses.
        </p>
      ) : (
        <ul className="provider-runtimes">
          {running.map((item) => {
            const reports = item.runtime.providerAccess;
            const usesSavedKey = reports?.some(
              (report) =>
                report.provider === "voyage" &&
                report.active?.source === "direct" &&
                report.active.key === "saved",
            );
            const needsRestart = reports?.some((report) => report.restartRequired);
            return (
              <li key={item.worktree}>
                <div className="provider-runtime-heading">
                  <strong>{item.name}</strong>
                  {item.runtime.version && (
                    <span className="hint">Rhizome {item.runtime.version}</span>
                  )}
                </div>
                <p className="path">{item.worktree}</p>
                {!reports?.length ? (
                  <p>
                    Unknown: this runtime does not report provider sources. Update its Rhizome
                    version and restart it.
                  </p>
                ) : (
                  reports.map((report) => (
                    <div className="provider-runtime-route" key={report.provider}>
                      <span>{providerNames[report.provider]}</span>
                      <div>
                        <p>
                          {report.active
                            ? `${report.provider === "typesafe" ? "Last evaluation connection" : "Active connection"}: ${routeText(report.active)}`
                            : report.provider === "typesafe"
                              ? "No evaluation connection yet"
                              : "No active connection"}
                        </p>
                        <p className="hint">
                          Next {report.provider === "typesafe" ? "evaluation" : "start"}:{" "}
                          {routeText(report.next)}
                        </p>
                        {report.active?.origin && <p className="path">{report.active.origin}</p>}
                      </div>
                    </div>
                  ))
                )}
                {(needsRestart || usesSavedKey) && (
                  <div className="provider-restart">
                    <p>
                      {needsRestart
                        ? "Restart required to apply the Voyage source change."
                        : "After replacing a saved Voyage key, restart this repository to use the new key."}
                    </p>
                    {item.runtime.mode === "headless" && item.repository ? (
                      <button
                        disabled={restarting.includes(item.worktree)}
                        onClick={() => void restart(item)}
                      >
                        {restarting.includes(item.worktree)
                          ? "Restarting…"
                          : `Restart ${item.name}`}
                      </button>
                    ) : (
                      <p className="hint">
                        {item.runtime.mode === "headless"
                          ? "Repository details are unavailable. Refresh Settings before restarting."
                          : "This runtime was started outside the app. Restart it where it was started."}
                      </p>
                    )}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {error && (
        <p role="alert" className="error-text">
          {error}
        </p>
      )}
    </div>
  );
}
