import { useEffect, useRef, useState } from "react";
import {
  failure,
  request,
  type Failure,
  type ScopeRule,
  type SetupChoices,
  type SetupPlan,
  type SetupResult,
} from "./api";
import { IndexScope, rulePath, type RuleAction } from "./IndexScope";
import { Elapsed, Notice } from "./Status";

/** How long the sheet waits after a change before asking for a fresh report. */
const REFRESH_MS = 250;

function choicesFrom(plan: SetupPlan): SetupChoices {
  return {
    workflow: plan.workflow,
    addons: plan.addons.filter((a) => a.enabled).map((a) => a.id),
    agents: plan.agents.filter((a) => a.enabled).map((a) => a.id),
    search: plan.search.provider,
    skip: [],
    keepIndexed: [],
    includeIgnored: plan.ignoredRepositories.filter((r) => r.included).map((r) => r.path),
  };
}

function toggle(list: string[], value: string, on: boolean) {
  return on ? [...list.filter((v) => v !== value), value] : list.filter((v) => v !== value);
}

/** "a", "a and b", "a, b, and c". */
function joinWords(words: string[]) {
  if (words.length < 3) return words.join(" and ");
  return `${words.slice(0, -1).join(", ")}, and ${words[words.length - 1]}`;
}

const detected = { marker: "in this repository", installed: "installed" };

/** The first-run setup of one unconfigured worktree: everything rzm init
 * asks in a terminal, preselected from its report, then one Set up. */
export function SetupSheet({
  repository,
  worktree,
  onOpen,
  onManageInstallation,
}: {
  repository: string;
  worktree: string;
  /** Continues into the normal open steps. */
  onOpen: () => void;
  onManageInstallation: () => void;
}) {
  const [plan, setPlan] = useState<SetupPlan | null>(null);
  const [choices, setChoices] = useState<SetupChoices | null>(null);
  const [problem, setProblem] = useState<Failure | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [key, setKey] = useState("");
  const [view, setView] = useState<"sheet" | "scope">("sheet");
  const [started, setStarted] = useState<number | null>(null);
  const [setupError, setSetupError] = useState<Failure | null>(null);
  const [result, setResult] = useState<SetupResult | null>(null);
  const alive = useRef(true);
  const reports = useRef(0);
  const edited = useRef(false);
  const id = repository;

  async function report(next?: SetupChoices) {
    const ticket = ++reports.current;
    setRefreshing(true);
    try {
      const fresh = await request(
        "setup-report",
        next ? { id, worktree, choices: next } : { id, worktree },
      );
      if (!alive.current || ticket !== reports.current) return;
      setPlan(fresh);
      setProblem(null);
      if (!next) setChoices(choicesFrom(fresh));
    } catch (cause) {
      if (alive.current && ticket === reports.current) setProblem(failure(cause));
    } finally {
      if (alive.current && ticket === reports.current) setRefreshing(false);
    }
  }

  useEffect(() => {
    alive.current = true;
    void report();
    return () => {
      alive.current = false;
    };
    // One sheet per worktree; the caller keys it by worktree.
  }, []);

  useEffect(() => {
    if (!choices || !edited.current) return;
    const timer = window.setTimeout(() => void report(choices), REFRESH_MS);
    return () => window.clearTimeout(timer);
  }, [choices]);

  function change(next: SetupChoices) {
    edited.current = true;
    setChoices(next);
  }

  async function setUp() {
    if (!choices) return;
    const typed = key.trim();
    // The key leaves the sheet only with this request, and only once.
    setKey("");
    setSetupError(null);
    setStarted(Date.now());
    try {
      const done = await request("initialize", {
        id,
        worktree,
        choices,
        ...(typed ? { key: typed } : {}),
      });
      if (alive.current) setResult(done.result);
    } catch (cause) {
      if (!alive.current) return;
      setSetupError(failure(cause));
      void report(choices);
    } finally {
      if (alive.current) setStarted(null);
    }
  }

  if (!plan || !choices) {
    if (!problem) return <Notice title="Checking what Rhizome would set up…" />;
    if (problem.code === "trust_required") {
      return (
        <Notice title="Trust this worktree?">
          <p>
            Setting up runs the Rhizome executable this worktree selects. Trust is recorded for this
            worktree only.
          </p>
          <p className="path confirmation-path">{worktree}</p>
          <button
            className="primary"
            disabled={refreshing}
            onClick={() =>
              void request("trust", { id, worktree })
                .then(() => report())
                .catch((cause) => setProblem(failure(cause)))
            }
          >
            Trust and continue
          </button>
        </Notice>
      );
    }
    const unsupported = problem.code === "setup_unsupported";
    return (
      <Notice
        title={
          unsupported
            ? "Rhizome needs an update to set up from the app"
            : "Could not check this folder"
        }
        failure={problem}
      >
        {unsupported && (
          <p>
            Update Rhizome under Manage installation, or run <code>rzm init</code> in a terminal in
            this folder.
          </p>
        )}
        <p className="path">{worktree}</p>
        <div className="actions">
          {(unsupported || problem.code === "missing_executable") && (
            <button onClick={onManageInstallation}>Manage installation</button>
          )}
          <button className="primary" disabled={refreshing} onClick={() => void report()}>
            Try again
          </button>
        </div>
      </Notice>
    );
  }

  if (result) {
    return (
      <section className="setup-sheet" aria-labelledby="setup-title">
        <h1 id="setup-title">Rhizome is set up in {plan.name}</h1>
        <p className="path">{plan.root}</p>
        <ul className="setup-summary">
          {result.summary.map((line) => (
            <li key={line}>
              <span className="check" aria-hidden="true">
                ✓
              </span>
              {line}
            </li>
          ))}
        </ul>
        {result.pin.error && (
          <p className="warning-text" role="alert">
            Rhizome {result.pin.version} could not be installed for this repository:{" "}
            {result.pin.error}. Opening the workspace tries again.
          </p>
        )}
        {result.kept.length > 0 && (
          <p className="hint">Left as they are: {joinWords(result.kept)}.</p>
        )}
        {result.warnings.map((warning) => (
          <p key={warning} className="warning-text">
            {warning}
          </p>
        ))}
        {result.searchHint && <p>{result.searchHint}</p>}
        <p>Commit {joinWords(result.commit)} so your team shares this setup.</p>
        <div className="setup-actions">
          <button className="primary" onClick={onOpen}>
            Open workspace
          </button>
          <button
            onClick={() =>
              void request("reveal", { worktree }).catch((cause) => setSetupError(failure(cause)))
            }
          >
            Reveal in Finder
          </button>
        </div>
      </section>
    );
  }

  const held = (rule: ScopeRule): RuleAction => {
    if (rule.layer !== "rhizome") return undefined;
    const path = rulePath(rule.pattern);
    const same = (value: string) => rulePath(value) === path;
    if (rule.included && choices.includeIgnored.some(same)) {
      return {
        label: "Undo",
        run: () =>
          change({ ...choices, includeIgnored: choices.includeIgnored.filter((v) => !same(v)) }),
      };
    }
    if (choices.skip.some(same)) {
      return {
        label: "Undo",
        run: () => change({ ...choices, skip: choices.skip.filter((v) => !same(v)) }),
      };
    }
    if (rule.reason && rule.planned) {
      return {
        label: "Keep indexed",
        run: () => change({ ...choices, keepIndexed: toggle(choices.keepIndexed, path, true) }),
      };
    }
    return undefined;
  };

  if (view === "scope") {
    return (
      <IndexScope
        name={plan.name}
        path={plan.root}
        scope={plan.scope}
        busy={refreshing}
        error={problem}
        note="Changes here become part of setup. Nothing is written until you choose Set up Rhizome."
        actionFor={held}
        kept={choices.keepIndexed.map((path) => ({
          path,
          undo: () => change({ ...choices, keepIndexed: toggle(choices.keepIndexed, path, false) }),
        }))}
        onSkip={(path) => change({ ...choices, skip: toggle(choices.skip, path, true) })}
        onInclude={(path) =>
          change({ ...choices, includeIgnored: toggle(choices.includeIgnored, path, true) })
        }
        onClose={() => setView("sheet")}
        closeLabel="Back to setup"
      />
    );
  }

  const provider = plan.search.providers.find((p) => p.id === choices.search);
  const needsKey = !!provider?.key && !provider.ready;
  const running = started !== null;

  return (
    <section className="setup-sheet" aria-labelledby="setup-title">
      <h1 id="setup-title">Set up Rhizome in {plan.name}</h1>
      <p className="path">{plan.root}</p>
      <p className="lede">
        Rhizome indexes this repository's docs and code so agents can search them, and installs
        guidance that teaches your agents to use it. Nothing is written until you choose Set up
        Rhizome.
      </p>
      <fieldset className="setup-form" disabled={running}>
        <div className="setup-row">
          <h2>Found</h2>
          <dl className="findings">
            <dt>Docs</dt>
            <dd>{plan.findings.docs}</dd>
            <dt>Code</dt>
            <dd>{plan.findings.code}</dd>
            <dt>Skip</dt>
            <dd>
              {plan.findings.skip ?? "nothing suggested"}{" "}
              <button className="link-button" onClick={() => setView("scope")}>
                What gets indexed…
              </button>
            </dd>
          </dl>
        </div>

        <div className="setup-row" role="radiogroup" aria-labelledby="setup-workflow">
          <h2 id="setup-workflow">Workflow</h2>
          <div className="choices">
            {plan.workflows.map((workflow) => (
              <label key={workflow.id} className="choice">
                <input
                  type="radio"
                  name="workflow"
                  checked={choices.workflow === workflow.id}
                  onChange={() =>
                    change({
                      ...choices,
                      workflow: workflow.id,
                      addons: plan.addons
                        .filter((a) => a.defaultFor.includes(workflow.id))
                        .map((a) => a.id),
                    })
                  }
                />
                <span className="choice-label">
                  {workflow.label}
                  {workflow.recommended && <span className="recommended">Recommended</span>}
                </span>
                <span className="choice-detail">{workflow.description}</span>
              </label>
            ))}
            {plan.addons.map((addon) => (
              <label key={addon.id} className="choice addon">
                <input
                  type="checkbox"
                  checked={choices.addons.includes(addon.id)}
                  onChange={(e) =>
                    change({
                      ...choices,
                      addons: toggle(choices.addons, addon.id, e.target.checked),
                    })
                  }
                />
                <span className="choice-label">{addon.label}</span>
                <span className="choice-detail">
                  {addon.description}.
                  {addon.defaultFor.includes(choices.workflow)
                    ? " Included with this workflow by default."
                    : ""}
                </span>
              </label>
            ))}
          </div>
        </div>

        <div className="setup-row" role="group" aria-labelledby="setup-agents">
          <h2 id="setup-agents">Agents</h2>
          <div>
            <div className="inline-choices">
              {plan.agents.map((agent) => (
                <label key={agent.id}>
                  <input
                    type="checkbox"
                    checked={choices.agents.includes(agent.id)}
                    onChange={(e) =>
                      change({
                        ...choices,
                        agents: toggle(choices.agents, agent.id, e.target.checked),
                      })
                    }
                  />
                  {agent.label}
                  {agent.detected && <span className="muted"> · {detected[agent.detected]}</span>}
                </label>
              ))}
            </div>
            <p className="hint">
              {choices.agents.length > 0
                ? "AGENTS.md and shared skills in .agents/ are written for every agent; most agents read them."
                : "No agent guidance is written."}
            </p>
          </div>
        </div>

        <div className="setup-row">
          <h2>
            <label htmlFor="setup-search">Search</label>
          </h2>
          <div>
            <div className="search-choice">
              <select
                id="setup-search"
                value={choices.search}
                onChange={(e) => change({ ...choices, search: e.target.value })}
              >
                {plan.search.providers.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </select>
              <span className={needsKey ? "warning-ink" : "muted"}>
                {choices.search === "off"
                  ? "Semantic search stays off; keyword search still works."
                  : provider?.ready
                    ? provider.key
                      ? "Key found"
                      : "Installed"
                    : provider?.key
                      ? "Needs a key"
                      : "Not installed"}
              </span>
            </div>
            {needsKey && provider && (
              <>
                <input
                  className="key-input"
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  aria-label={
                    provider.teamKey
                      ? `Atomic Object Rhizome key or ${provider.keyLabel}`
                      : provider.keyLabel
                  }
                  placeholder={
                    provider.teamKey
                      ? `Atomic Object Rhizome key or ${provider.keyLabel}`
                      : provider.keyLabel
                  }
                  value={key}
                  onChange={(e) => setKey(e.target.value)}
                />
                <p className="hint">
                  Saved to ~/.config/rhizome/config.yml when you set up, never in this repository.
                  Leave it empty to set up without semantic search and add a key later.
                </p>
              </>
            )}
            {!provider?.ready && provider?.hint && <p className="hint">{provider.hint}</p>}
          </div>
        </div>

        {plan.ignoredRepositories.length > 0 && (
          <div className="setup-row" role="group" aria-labelledby="setup-nested">
            <h2 id="setup-nested">Ignored repositories</h2>
            <div className="choices">
              {plan.ignoredRepositories.map((repo) => (
                <label key={repo.path} className="choice">
                  <input
                    type="checkbox"
                    checked={choices.includeIgnored.includes(repo.path)}
                    onChange={(e) =>
                      change({
                        ...choices,
                        includeIgnored: toggle(choices.includeIgnored, repo.path, e.target.checked),
                      })
                    }
                  />
                  <span className="choice-label">
                    Index <code>{repo.path}</code>
                  </span>
                  <span className="choice-detail">
                    A Git repository inside this one that Git ignores
                    {repo.source ? ` (${repo.source}:${repo.line})` : ""}.
                  </span>
                </label>
              ))}
            </div>
          </div>
        )}

        <div className="setup-row">
          <h2>Writes</h2>
          <div className={refreshing ? "writes refreshing" : "writes"} aria-busy={refreshing}>
            <ul className="writes-summary">
              {plan.writes.summary.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
            <details>
              <summary>
                All {plan.writes.files.length} files
                {refreshing && <span className="muted"> · updating…</span>}
              </summary>
              <ul className="file-list">
                {plan.writes.files.map((file) => (
                  <li key={file}>{file}</li>
                ))}
              </ul>
            </details>
          </div>
        </div>
      </fieldset>

      {problem && (
        <p className="error-text" role="alert">
          {problem.message}
        </p>
      )}
      {setupError && (
        <div className="setup-error" role="alert">
          <p className="error-text">Setup failed: {setupError.message}</p>
          <button onClick={onOpen}>Open workspace</button>
        </div>
      )}
      <div className="setup-actions">
        {running ? (
          <span className="setup-running" role="status" aria-live="polite">
            <span className="spinner" aria-hidden="true" />
            Setting up Rhizome… <Elapsed started={started} />
          </span>
        ) : (
          <button
            className="primary"
            disabled={refreshing || !!problem}
            onClick={() => void setUp()}
          >
            {setupError ? "Try again" : "Set up Rhizome"}
          </button>
        )}
        <span className="hint">
          {plan.pin ? `Pins Rhizome ${plan.pin} for this repository and trusts` : "Trusts"} this
          worktree to run it.
        </span>
      </div>
    </section>
  );
}
