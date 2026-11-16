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

const marks: Record<Readiness, string> = {
  ready: "connected",
  "signed-out": "legacy",
  unchecked: "legacy",
  missing: "unavailable",
};

function statusText(state: Readiness, status: ChatHarnessStatus) {
  switch (state) {
    case "ready":
      return status.version ? `Ready · ${status.version}` : "Ready";
    case "signed-out":
      return status.loginHint ? (
        <>
          Signed out. Run <code>{status.loginHint}</code> in a terminal.
        </>
      ) : (
        "Signed out."
      );
    case "unchecked":
      return "Could not check its status.";
    case "missing":
      return "Not installed";
  }
}

/** Claude Code lists its default as a model named `default`; Codex flags one. */
function defaultLabel(status: ChatHarnessStatus) {
  const models = status.models ?? [];
  const own = models.find((option) => option.id === "default");
  if (own?.displayName) return own.displayName;
  const flagged = models.find((option) => option.default);
  return flagged ? `Default (${flagged.displayName || flagged.id})` : "Default";
}

/** Chooses the coding agent, and its model, that chat in every repository uses. */
export function ChatSettings() {
  const [report, setReport] = useState<ChatSettingsReport | null>(null);
  const [checking, setChecking] = useState(true);
  const [saving, setSaving] = useState(false);
  const [copied, setCopied] = useState<ChatHarness | null>(null);
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
  async function save(harness: ChatHarness, model?: string) {
    setSaving(true);
    await apply(() =>
      request("chat-settings-set", model === undefined ? { harness } : { harness, model }),
    );
    if (alive.current) setSaving(false);
  }
  async function copy(harness: ChatHarness, hint: string) {
    try {
      await navigator.clipboard.writeText(hint);
      setCopied(harness);
    } catch {
      setError("Could not copy. Select the command and copy it instead.");
    }
  }
  async function openInstall(harness: ChatHarness) {
    try {
      await request("open-chat-install", { harness });
    } catch (cause) {
      setError(failure(cause).message);
    }
  }
  const chosen = report?.settings.harness ?? report?.resolved.harness;
  const rows = order.flatMap((kind) => {
    const found = report?.harnesses.find((item) => item.kind === kind);
    return found ? [found] : [];
  });
  return (
    <section className="setting-group chat-settings" aria-labelledby="chat-settings-title">
      <h2 id="chat-settings-title">Chat</h2>
      <p>Chat runs a coding agent installed on this Mac. Choose which one every repository uses.</p>
      {report ? (
        <div className="chat-agents" role="radiogroup" aria-label="Chat agent">
          {rows.map(({ kind, status }) => {
            const state = readiness(status);
            const models = (status.models ?? []).filter((option) => option.id !== "default");
            const saved = report.settings.harnesses[kind]?.model ?? "";
            const model = models.some((option) => option.id === saved) ? saved : "";
            return (
              <div className="chat-agent" key={kind}>
                <input
                  type="radio"
                  id={`chat-agent-${kind}`}
                  name="chat-agent"
                  checked={chosen === kind}
                  disabled={saving || state === "missing"}
                  onChange={() => void save(kind)}
                />
                <div>
                  <label htmlFor={`chat-agent-${kind}`}>{names[kind]}</label>
                  <p className="provider-source-status">
                    <span className={`provider-status-mark ${marks[state]}`} aria-hidden="true" />
                    <span>{statusText(state, status)}</span>
                  </p>
                  {state === "unchecked" && <p className="hint">{status.lastError}</p>}
                </div>
                {state === "signed-out" && status.loginHint && (
                  <button onClick={() => void copy(kind, status.loginHint ?? "")}>
                    {copied === kind ? "Copied" : "Copy"}
                  </button>
                )}
                {state === "missing" && (
                  <button onClick={() => void openInstall(kind)}>Install Guide</button>
                )}
                {chosen === kind && models.length > 0 && (
                  <div className="chat-model">
                    <label htmlFor="chat-model">Model</label>
                    <select
                      id="chat-model"
                      value={model}
                      disabled={saving}
                      onChange={(e) => void save(kind, e.target.value)}
                    >
                      <option value="">{defaultLabel(status)}</option>
                      {models.map((option) => (
                        <option key={option.id} value={option.id}>
                          {option.displayName || option.id}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
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
