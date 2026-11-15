import { useCallback, useEffect, useRef, useState } from "react";
import {
  failure,
  request,
  type ChatHarness,
  type ChatHarnessStatus,
  type ChatSettingsReport,
} from "./api";

const names: Record<ChatHarness, string> = { claude: "Claude Code", codex: "Codex" };
const order: ChatHarness[] = ["claude", "codex"];

type Readiness = "ready" | "signed-out" | "unchecked" | "missing";

function readiness(status: ChatHarnessStatus): Readiness {
  if (!status.installed) return "missing";
  if (status.loggedIn) return "ready";
  return status.lastError ? "unchecked" : "signed-out";
}

/**
 * The coding agents chat runs on this Mac: whether each can run, and a switch
 * that hides one from chat's model picker. Detected agents are on until
 * turned off; the picker in the chat chooses among the ones that are on.
 */
export function ChatSettings() {
  const [report, setReport] = useState<ChatSettingsReport | null>(null);
  const [checking, setChecking] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const alive = useRef(true);
  const revision = useRef(0);
  async function apply(action: () => Promise<ChatSettingsReport>) {
    const current = ++revision.current;
    setError("");
    try {
      const fresh = await action();
      if (alive.current && current === revision.current) setReport(fresh);
    } catch (cause) {
      if (alive.current && current === revision.current) setError(failure(cause).message);
    }
  }
  const check = useCallback(async () => {
    setChecking(true);
    await apply(() => request("chat-settings", {}));
    if (alive.current) setChecking(false);
  }, []);
  useEffect(() => {
    alive.current = true;
    void check();
    return () => {
      alive.current = false;
    };
  }, [check]);
  async function turn(harness: ChatHarness, enabled: boolean) {
    setSaving(true);
    await apply(() => request("chat-settings-set", { harness, enabled }));
    if (alive.current) setSaving(false);
  }
  async function openInstall(harness: ChatHarness) {
    try {
      await request("open-chat-install", { harness });
    } catch (cause) {
      setError(failure(cause).message);
    }
  }
  function statusText(state: Readiness, kind: ChatHarness, status: ChatHarnessStatus) {
    switch (state) {
      case "ready":
        return "Ready";
      case "signed-out":
        return status.loginHint ? (
          <>
            Signed out. Run <code>{status.loginHint}</code> in Terminal, then check again.
          </>
        ) : (
          "Signed out. Sign in to it in Terminal, then check again."
        );
      case "unchecked":
        return "Could not check its status.";
      case "missing":
        return (
          <>
            Not installed.{" "}
            <button className="link-button" onClick={() => void openInstall(kind)}>
              How to install
            </button>
          </>
        );
    }
  }
  const rows = order.flatMap((kind) => {
    const found = report?.harnesses.find((item) => item.kind === kind);
    return found ? [found] : [];
  });
  return (
    <section className="setting-group chat-settings" aria-labelledby="chat-settings-title">
      <h2 id="chat-settings-title">Chat</h2>
      <p>Chat runs the coding agents installed on this Mac, signed in with your own accounts.</p>
      {report ? (
        <div className="chat-agents">
          {rows.map(({ kind, status }) => {
            const state = readiness(status);
            const on = status.installed && !report.settings.harnesses[kind]?.disabled;
            return (
              <div className={`chat-agent ${state}`} key={kind}>
                <span className="chat-agent-name">{names[kind]}</span>
                <button
                  type="button"
                  role="switch"
                  className="chat-switch"
                  aria-checked={on}
                  aria-label={`Use ${names[kind]} in chat`}
                  disabled={saving || !status.installed}
                  onClick={() => void turn(kind, !on)}
                />
                <p className="chat-agent-status">
                  <span className={`chat-mark ${state}`} aria-hidden="true" />
                  <span>{statusText(state, kind, status)}</span>
                </p>
                {state === "unchecked" && <p className="hint">{status.lastError}</p>}
              </div>
            );
          })}
        </div>
      ) : (
        !error && <p>Checking coding agents…</p>
      )}
      <div className="actions chat-check">
        <button disabled={checking || saving} onClick={() => void check()}>
          {checking ? "Checking…" : "Check Again"}
        </button>
      </div>
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
    </section>
  );
}
