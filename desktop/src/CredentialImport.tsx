import { useEffect, useRef, useState } from "react";
import { failure, request, type CredentialSource, type CredentialStatus } from "./api";

const providers = [
  ["VOYAGE_API_KEY", "Voyage"],
  ["TYPESAFE_API_KEY", "TypeSafe"],
  ["OPENAI_API_KEY", "OpenAI"],
  ["CEREBRAS_API_KEY", "Cerebras"],
  ["ANTHROPIC_API_KEY", "Anthropic"],
] as const;
const defaults: CredentialSource = {
  account: "",
  environment: "",
  keys: ["VOYAGE_API_KEY", "TYPESAFE_API_KEY"],
};

export function CredentialImport({
  onImported,
  disabled = false,
}: {
  onImported?: () => void;
  disabled?: boolean;
}) {
  const [status, setStatus] = useState<CredentialStatus | null>(null);
  const [source, setSource] = useState<CredentialSource>(defaults);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const alive = useRef(true);
  const edited = useRef(false);
  const importing = useRef(false);
  const canceled = useRef(false);
  useEffect(() => {
    alive.current = true;
    void request("credential-status", {})
      .then((fresh) => {
        if (!alive.current) return;
        setStatus(fresh);
        if (fresh.source && !edited.current) setSource(fresh.source);
      })
      .catch((cause) => {
        if (alive.current) setError(failure(cause).message);
      });
    return () => {
      alive.current = false;
      if (importing.current) void request("credential-cancel", {}).catch(() => {});
    };
  }, []);

  async function importKeys() {
    setBusy(true);
    importing.current = true;
    canceled.current = false;
    setError("");
    setMessage("");
    try {
      const fresh = await request("credential-import", {
        source: {
          ...source,
          account: source.account.trim(),
          environment: source.environment.trim(),
        },
      });
      if (!alive.current) return;
      setStatus(fresh);
      setMessage(
        "Provider keys imported into your private user configuration. Environment variables still take precedence.",
      );
      onImported?.();
    } catch (cause) {
      if (alive.current) {
        if (canceled.current) setMessage("Import canceled. Checking the saved provider status…");
        else setError(failure(cause).message);
      }
      try {
        const fresh = await request("credential-status", {});
        if (alive.current) {
          setStatus(fresh);
          onImported?.();
        }
      } catch (refreshCause) {
        if (alive.current) setError(failure(refreshCause).message);
      }
    } finally {
      importing.current = false;
      if (alive.current) setBusy(false);
    }
  }
  async function cancel() {
    canceled.current = true;
    try {
      await request("credential-cancel", {});
    } catch (cause) {
      if (alive.current) setError(failure(cause).message);
    }
  }

  return (
    <div className="setting-group">
      <h2>Import provider keys from 1Password</h2>
      <p>
        Atomic Object setup uses Voyage and TypeSafe. Install stable 1Password CLI 2.41 or newer,
        unlock the 1Password app, sign in to your account, and enable CLI integration under Settings
        → Developer. Your account needs read access to the Environment.
      </p>
      {status && !status.available && (
        <p className="hint">
          The op command was not found. Install 1Password CLI in your PATH, /opt/homebrew/bin, or
          /usr/local/bin, then reopen Rhizome.
        </p>
      )}
      {status && status.ready.length > 0 && (
        <p>
          Keys available:{" "}
          {providers
            .filter(([key]) => status.ready.includes(key))
            .map(([, label]) => label)
            .join(", ")}
          .
        </p>
      )}
      <fieldset
        disabled={busy || disabled}
        onChange={() => {
          edited.current = true;
        }}
      >
        <label>
          1Password account
          <input
            autoComplete="off"
            value={source.account}
            onChange={(e) => setSource({ ...source, account: e.target.value })}
            placeholder="example.1password.com"
          />
        </label>
        <label>
          Environment ID
          <input
            autoComplete="off"
            spellCheck={false}
            value={source.environment}
            onChange={(e) => setSource({ ...source, environment: e.target.value })}
          />
        </label>
        <p className="hint">
          In 1Password, choose Developer → View Environments → View environment → Manage environment
          → Copy environment ID. Ask your team for the shared Environment.
        </p>
        <div className="inline-choices">
          {providers.map(([key, label]) => (
            <label key={key}>
              <input
                type="checkbox"
                checked={source.keys.includes(key)}
                onChange={(e) =>
                  setSource({
                    ...source,
                    keys: e.target.checked
                      ? [...source.keys, key]
                      : source.keys.filter((value) => value !== key),
                  })
                }
              />
              {label}
            </label>
          ))}
        </div>
        <p className="hint">
          Only selected provider keys are saved. Keys are stored as plaintext in your user
          configuration with private file permissions. The account and Environment ID are remembered
          on this computer for re-import after rotation.
        </p>
        <button
          disabled={
            !source.account.trim() || !source.environment.trim() || source.keys.length === 0
          }
          onClick={() => void importKeys()}
        >
          {status?.source ? "Re-import selected keys" : "Import selected keys"}
        </button>
      </fieldset>
      {busy && (
        <div className="actions">
          <span role="status">
            Waiting for 1Password approval… Import times out after 90 seconds.
          </span>
          <button onClick={() => void cancel()}>Cancel import</button>
        </div>
      )}
      {message && <p role="status">{message}</p>}
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
