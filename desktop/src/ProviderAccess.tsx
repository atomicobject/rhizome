import { useCallback, useEffect, useRef, useState } from "react";
import {
  failure,
  request,
  type CredentialStatus,
  type Provider,
  type ProviderPreference,
  type ProviderReport,
} from "./api";
import { CredentialImport } from "./CredentialImport";
import { ProviderKey } from "./ProviderKey";
import { ProviderRuntimes, providerNames, routeText, type ProviderRuntime } from "./ProviderRoutes";
import { TeamAccount } from "./TeamAccount";

const listeners = new Set<() => void>();
/** The app broadcasts successful provider-setting writes across its windows. */
export function providerAccessChanged() {
  for (const listener of listeners) listener();
}

export function ProviderAccess({
  version,
  repositories = [],
  onRestart = async () => {},
}: {
  version?: string;
  repositories?: ProviderRuntime[];
  onRestart?: (repository: string, worktree: string) => Promise<void>;
}) {
  const [status, setStatus] = useState<CredentialStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<{
    provider: Provider;
    preference: ProviderPreference;
  } | null>(null);
  const alive = useRef(true);
  const revision = useRef(0);
  const refresh = useCallback(() => {
    const current = ++revision.current;
    void request("credential-status", {})
      .then((fresh) => {
        if (alive.current && current === revision.current) {
          setStatus(fresh);
          setError("");
        }
      })
      .catch((cause) => {
        if (alive.current && current === revision.current) setError(failure(cause).message);
      });
  }, []);
  useEffect(() => {
    alive.current = true;
    listeners.add(refresh);
    window.addEventListener("focus", refresh);
    refresh();
    return () => {
      alive.current = false;
      listeners.delete(refresh);
      window.removeEventListener("focus", refresh);
    };
  }, [refresh]);

  async function update(action: () => Promise<CredentialStatus>) {
    revision.current++;
    setBusy(true);
    setError("");
    try {
      const fresh = await action();
      if (alive.current) {
        revision.current++;
        setStatus(fresh);
      }
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  async function choose(provider: Provider, preference: ProviderPreference) {
    setPending({ provider, preference });
    try {
      await update(() => request("provider-preference-set", { provider, preference }));
    } catch (cause) {
      if (alive.current) setError(failure(cause).message);
    } finally {
      if (alive.current) setPending(null);
    }
  }
  async function saveKey(report: ProviderReport, key?: string) {
    await update(() =>
      key === undefined
        ? request("provider-key-clear", { provider: report.provider })
        : request("provider-key-set", { provider: report.provider, key }),
    );
  }
  const providers = status?.providers;
  return (
    <section className="setting-group provider-access" aria-labelledby="provider-access-title">
      <h2 id="provider-access-title">Provider access</h2>
      <p>
        Voyage runs semantic search and indexing. Jev runs evaluation. Choose where each provider
        sends its requests.
      </p>
      <TeamAccount compact onChange={refresh} />
      <div className="provider-subsection">
        <h3>Source for new connections</h3>
        <p className="hint">
          Reported by this app’s Rhizome{version ? ` ${version}` : ""}. Running repositories report
          their own connections below.
        </p>
        {providers?.length ? (
          <>
            {providers.map((report) => {
              const preference =
                pending?.provider === report.provider ? pending.preference : report.preference;
              return (
                <div
                  className="provider-route"
                  key={report.provider}
                  aria-busy={pending?.provider === report.provider}
                >
                  <div>
                    <strong>{providerNames[report.provider]}</strong>
                    <p className="hint">
                      {report.provider === "voyage"
                        ? "Semantic search and indexing"
                        : "Evaluation (TypeSafe)"}
                    </p>
                  </div>
                  <div
                    className="segmented"
                    role="radiogroup"
                    aria-label={`${providerNames[report.provider]} source`}
                  >
                    {(["server", "key"] satisfies ProviderPreference[]).map((choice) => (
                      <label key={choice}>
                        <input
                          type="radio"
                          name={`provider-source-${report.provider}`}
                          value={choice}
                          checked={preference === choice}
                          disabled={busy}
                          onChange={() => void choose(report.provider, choice)}
                        />
                        {choice === "server" ? "Atomic Object server" : "Your key"}
                      </label>
                    ))}
                  </div>
                  <div className="provider-route-description">
                    <p className="provider-source-status">
                      <span
                        className={`provider-status-mark ${report.source === "none" ? "unavailable" : report.source === "bundle" ? "legacy" : "connected"}`}
                        aria-hidden="true"
                      />
                      <span>
                        {!report.preference && "Not chosen · "}
                        {pending?.provider === report.provider
                          ? "Saving source…"
                          : routeText(report)}
                      </span>
                    </p>
                    {report.origin && <p className="path">{report.origin}</p>}
                    {report.preference && (
                      <button
                        className="text-button"
                        disabled={busy}
                        onClick={() => void choose(report.provider, "")}
                      >
                        Use automatic selection for {providerNames[report.provider]}
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
            <p className="hint">
              Choosing the server overrides saved and environment keys. If the server is
              unavailable, requests fail. Signing in and importing keys do not change your choice.
            </p>
            <p className="hint">
              Voyage keeps its connection until the repository restarts. Jev checks its source on
              each evaluation; account changes can take about 30 seconds.
            </p>
          </>
        ) : (
          <p>
            {status
              ? "Provider sources are unavailable from this Rhizome version. Update the app to manage them."
              : "Checking provider sources…"}
          </p>
        )}
      </div>
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
      <ProviderRuntimes repositories={repositories} onRestart={onRestart} />
      {providers?.length ? (
        <div className="provider-subsection">
          <h3>Keys on this Mac</h3>
          <p className="hint">
            Keys are saved as plaintext in your private user configuration. Existing key values are
            never displayed.
          </p>
          {providers.map((report) => (
            <ProviderKey
              key={report.provider}
              report={report}
              disabled={busy}
              onSave={(key) => saveKey(report, key)}
              onClear={() => saveKey(report)}
            />
          ))}
        </div>
      ) : null}
      <details className="provider-import-disclosure">
        <summary>
          Import from 1Password
          {status?.source?.account ? ` · last import ${status.source.account}` : ""}
        </summary>
        <p className="hint">Import adds selected keys. Choose Your key above to use them.</p>
        <CredentialImport compact disabled={busy} onImported={refresh} />
      </details>
    </section>
  );
}
