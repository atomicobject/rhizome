import { useEffect, useRef, useState } from "react";
import { failure, request, type TeamStatus } from "./api";

const listeners = new Set<(url: string) => void>();
/** Called by the shell for each sign-in progress message from the app. */
export function teamProgress(url: string) {
  for (const listener of listeners) listener(url);
}

function expiry(value?: string): string {
  const date = new Date(value ?? "");
  return Number.isNaN(date.getTime())
    ? (value ?? "")
    : date.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/** The Atomic Object team account (SPEC-0129 US5). `setup` adds the key-import alternative. */
export function TeamAccount({
  setup = false,
  disabled = false,
  onChange,
}: {
  setup?: boolean;
  disabled?: boolean;
  onChange?: () => void;
}) {
  const [status, setStatus] = useState<TeamStatus | null>(null);
  const [busy, setBusy] = useState<"sign-in" | "sign-out" | null>(null);
  const [url, setUrl] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const alive = useRef(true);
  const signingIn = useRef(false);
  const canceled = useRef(false);
  useEffect(() => {
    alive.current = true;
    const listener = (next: string) => {
      if (signingIn.current) setUrl(next);
    };
    listeners.add(listener);
    void request("team-status", {})
      .then((fresh) => {
        if (alive.current) setStatus(fresh);
      })
      .catch((cause) => {
        if (alive.current) setError(failure(cause).message);
      });
    return () => {
      alive.current = false;
      listeners.delete(listener);
      if (signingIn.current) void request("team-cancel", {}).catch(() => {});
    };
  }, []);

  async function signIn() {
    setBusy("sign-in");
    signingIn.current = true;
    canceled.current = false;
    setUrl("");
    setError("");
    setMessage("");
    try {
      const fresh = await request("team-sign-in", {});
      if (!alive.current) return;
      setStatus(fresh);
      onChange?.();
    } catch (cause) {
      if (!alive.current) return;
      if (canceled.current) setMessage("Sign-in canceled.");
      else setError(failure(cause).message);
    } finally {
      signingIn.current = false;
      if (alive.current) setBusy(null);
    }
  }
  async function cancel() {
    canceled.current = true;
    try {
      await request("team-cancel", {});
    } catch (cause) {
      if (alive.current) setError(failure(cause).message);
    }
  }
  async function signOut() {
    setBusy("sign-out");
    setError("");
    setMessage("");
    try {
      const result = await request("team-sign-out", {});
      if (!alive.current) return;
      setStatus({ signed_in: false, origin: status?.origin });
      if (!result.remote_revoked)
        setMessage(
          "Signed out on this Mac. Rhizome could not reach the team server, so the session stays valid there until it expires.",
        );
      onChange?.();
    } catch (cause) {
      if (alive.current) setError(failure(cause).message);
    } finally {
      if (alive.current) setBusy(null);
    }
  }

  return (
    <div className="setting-group">
      <h2>Team account</h2>
      {busy === "sign-in" ? (
        <>
          <div className="actions">
            <span role="status">Waiting for the browser…</span>
            <button onClick={() => void cancel()}>Cancel</button>
          </div>
          {url && (
            <p className="hint">
              If no browser opened, visit <span className="path">{url}</span>
            </p>
          )}
        </>
      ) : status?.signed_in ? (
        <>
          <p>
            Signed in as <strong>{status.email}</strong>.
            {status.session_expires_at && ` Session expires ${expiry(status.session_expires_at)}.`}
          </p>
          <div className="actions">
            <button disabled={disabled || busy !== null} onClick={() => void signOut()}>
              Sign out
            </button>
          </div>
        </>
      ) : (
        <>
          <p>
            Uses the team's Voyage and Jev access without storing provider keys on this Mac.
            {setup && " Or import your own keys from 1Password below."}
          </p>
          <div className="actions">
            <button
              className="primary"
              disabled={disabled || busy !== null || (!status && !error)}
              onClick={() => void signIn()}
            >
              Sign in with Atomic Object
            </button>
          </div>
        </>
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
