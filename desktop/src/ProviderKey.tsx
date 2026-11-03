import { useEffect, useRef, useState } from "react";
import { failure, type ProviderReport } from "./api";

export function ProviderKey({
  report,
  disabled,
  onSave,
  onClear,
}: {
  report: ProviderReport;
  disabled: boolean;
  onSave: (key: string) => Promise<void>;
  onClear: () => Promise<void>;
}) {
  const label = report.provider === "voyage" ? "Voyage" : "TypeSafe";
  const [mode, setMode] = useState<"idle" | "edit" | "clear">("idle");
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const opener = useRef<HTMLButtonElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (mode === "edit") input.current?.focus();
    if (mode === "clear") cancel.current?.focus();
  }, [mode]);
  function close() {
    setKey("");
    setMode("idle");
    requestAnimationFrame(() => opener.current?.focus());
  }
  async function save() {
    setBusy(true);
    setError("");
    try {
      await onSave(key.trim());
      close();
    } catch (cause) {
      setError(failure(cause).message);
    } finally {
      setKey("");
      setBusy(false);
    }
  }
  async function clear() {
    setBusy(true);
    setError("");
    try {
      await onClear();
      close();
    } catch (cause) {
      setError(failure(cause).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="provider-key">
      <div className="provider-key-heading">
        <div>
          <strong>{label} key</strong>
          <p className="hint">
            {report.saved ? "Saved in your private user configuration" : "No key saved"}
          </p>
          {report.key === "environment" && (
            <p className="hint">
              {report.envName || "API key"} is set in the environment.{" "}
              {report.saved && "It takes precedence over the saved key when using Your key."}
            </p>
          )}
        </div>
        {mode === "idle" && (
          <div className="actions">
            <button
              ref={opener}
              disabled={disabled}
              onClick={() => {
                setError("");
                setMode("edit");
              }}
            >
              {report.saved ? `Replace ${label} key…` : `Add ${label} key…`}
            </button>
            {report.saved && (
              <button
                disabled={disabled}
                onClick={() => {
                  setError("");
                  setMode("clear");
                }}
              >
                Clear {label} key…
              </button>
            )}
          </div>
        )}
      </div>
      {mode === "edit" && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (!busy && !disabled && key.trim()) void save();
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape" && !busy) {
              event.preventDefault();
              event.stopPropagation();
              close();
            }
          }}
        >
          <label htmlFor={`provider-key-${report.provider}`}>New {label} API key</label>
          <div className="input-actions">
            <input
              id={`provider-key-${report.provider}`}
              ref={input}
              type="password"
              autoComplete="off"
              spellCheck={false}
              value={key}
              disabled={busy}
              onChange={(event) => setKey(event.target.value)}
            />
            <button type="submit" disabled={busy || disabled || !key.trim()}>
              {busy ? "Saving…" : "Save key"}
            </button>
            <button type="button" disabled={busy} onClick={close}>
              Cancel
            </button>
          </div>
        </form>
      )}
      {mode === "clear" && (
        <div className="provider-key-confirmation">
          <p>
            Clear the saved {label} key? Choosing Your key will require another saved key or an
            environment key.
          </p>
          <div className="actions">
            <button disabled={busy || disabled} onClick={() => void clear()}>
              {busy ? "Clearing…" : "Clear key"}
            </button>
            <button ref={cancel} disabled={busy} onClick={close}>
              Cancel
            </button>
          </div>
        </div>
      )}
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
