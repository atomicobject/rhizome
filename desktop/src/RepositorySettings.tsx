import { useEffect, useRef, useState } from "react";
import {
  failure,
  request,
  type Failure,
  type SettingsChoices,
  type SetupPlan,
  type SetupResult,
} from "./api";
import { onProviderAccessChanged } from "./ProviderAccess";
import { Elapsed } from "./Status";
import { RepositoryRhizome, type RepositoryRhizomeProps } from "./RepositoryRhizome";

/** How long the tab waits after a change before asking for a fresh report. */
const REFRESH_MS = 250;

/** What the person changed: unchanged settings are left out of requests. */
interface Draft {
  workflow: string;
  agents: string[];
  search: string;
  add: string[];
  remove: string[];
}

type Row = "workflow" | "addons" | "agents" | "search";

function draftFrom(plan: SetupPlan): Draft {
  return {
    workflow: plan.workflow,
    agents: plan.agents.filter((a) => a.enabled).map((a) => a.id),
    search: plan.search.provider,
    add: [],
    remove: [],
  };
}

const sameSet = (a: string[], b: string[]) =>
  a.length === b.length && a.every((value) => b.includes(value));

function changedRows(saved: Draft, draft: Draft): Row[] {
  const rows: Row[] = [];
  if (draft.workflow !== saved.workflow) rows.push("workflow");
  if (draft.add.length + draft.remove.length > 0) rows.push("addons");
  if (!sameSet(draft.agents, saved.agents)) rows.push("agents");
  if (draft.search !== saved.search) rows.push("search");
  return rows;
}

function choicesFor(saved: Draft, draft: Draft): SettingsChoices {
  const rows = changedRows(saved, draft);
  return {
    ...(rows.includes("workflow") ? { workflow: draft.workflow } : {}),
    ...(rows.includes("agents") ? { agents: draft.agents } : {}),
    ...(rows.includes("search") ? { search: draft.search } : {}),
    ...(draft.add.length ? { addAddons: draft.add } : {}),
    ...(draft.remove.length ? { removeAddons: draft.remove } : {}),
  };
}

function without(list: string[], value: string) {
  return list.filter((v) => v !== value);
}

function plural(n: number, noun: string) {
  return `${n} ${noun}${n === 1 ? "" : "s"}`;
}

/** "a", "a and b", "a, b, and c". */
function joinWords(words: string[]) {
  if (words.length < 3) return words.join(" and ");
  return `${words.slice(0, -1).join(", ")}, and ${words[words.length - 1]}`;
}

const detected = { marker: "in this repository", installed: "installed" };

function ChangedMark() {
  return <span className="changed-mark" role="img" aria-label="Changed" />;
}

/** A set-up repository's workflow, add-ons, agents, and search provider,
 * read from and applied by the worktree's own Rhizome (SPEC-0118 US3).
 * Changes are staged until Apply changes. */
export function RepositorySettings({
  repository,
  worktree,
  onEditRules,
  rhizome,
  onProviderSettings,
  onChanged,
}: {
  repository: string;
  worktree: string;
  /** Opens What gets indexed for this worktree. */
  onEditRules: () => void;
  rhizome: RepositoryRhizomeProps;
  /** Shows the app's provider settings, where keys are saved. */
  onProviderSettings: () => void;
  /** Reports whether there are changes that have not been applied. */
  onChanged?: (changed: boolean) => void;
}) {
  /** The repository's settings as they are, and what the person changed. */
  const [current, setCurrent] = useState<SetupPlan | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  /** The latest report for the draft: what applying it would write. */
  const [plan, setPlan] = useState<SetupPlan | null>(null);
  const [problem, setProblem] = useState<Failure | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [changingVersion, setChangingVersion] = useState(false);
  const [trustedHere, setTrustedHere] = useState(false);
  const [started, setStarted] = useState<number | null>(null);
  const [applyError, setApplyError] = useState<Failure | null>(null);
  const [result, setResult] = useState<SetupResult | null>(null);
  const alive = useRef(true);
  const reports = useRef(0);
  const id = repository;
  const saved = current && draftFrom(current);
  const rows = saved && draft ? changedRows(saved, draft) : [];
  const running = started !== null;

  async function load() {
    const ticket = ++reports.current;
    setRefreshing(true);
    try {
      const fresh = await request("settings-report", { id, worktree });
      if (!alive.current || ticket !== reports.current) return;
      setCurrent(fresh);
      setPlan(fresh);
      setDraft(draftFrom(fresh));
      setProblem(null);
    } catch (cause) {
      if (alive.current && ticket === reports.current) setProblem(failure(cause));
    } finally {
      if (alive.current && ticket === reports.current) setRefreshing(false);
    }
  }

  useEffect(() => {
    alive.current = true;
    void load();
    return () => {
      alive.current = false;
    };
    // One tab per worktree; the caller keys it by worktree.
  }, []);

  useEffect(() => onChanged?.(rows.length > 0), [rows.length > 0]);

  // A key saved on the app tab changes provider readiness; keep the draft.
  useEffect(
    () =>
      onProviderAccessChanged(() => {
        request("settings-report", { id, worktree })
          .then((fresh) => {
            if (!alive.current) return;
            setCurrent(fresh);
            setDraft((d) => d && { ...d });
          })
          .catch(() => {});
      }),
    [],
  );

  // A changed draft asks for a fresh report of what it would write.
  useEffect(() => {
    if (!saved || !draft || !current) return;
    if (rows.length === 0) {
      // A report still on its way would describe changes that are gone.
      reports.current++;
      setRefreshing(false);
      setProblem(null);
      setPlan(current);
      return;
    }
    const ticket = ++reports.current;
    setRefreshing(true);
    const timer = window.setTimeout(() => {
      request("settings-report", { id, worktree, choices: choicesFor(saved, draft) })
        .then((fresh) => {
          if (!alive.current || ticket !== reports.current) return;
          setPlan(fresh);
          setProblem(null);
        })
        .catch((cause) => {
          if (alive.current && ticket === reports.current) setProblem(failure(cause));
        })
        .finally(() => {
          if (alive.current && ticket === reports.current) setRefreshing(false);
        });
    }, REFRESH_MS);
    return () => window.clearTimeout(timer);
  }, [draft]);

  function change(next: Draft) {
    setResult(null);
    setApplyError(null);
    // On the current workflow, an add-on override that matches the
    // repository changes nothing, so it is dropped.
    if (current && next.workflow === current.workflow) {
      const enabled = (id: string) => !!current.addons.find((a) => a.id === id)?.enabled;
      next = {
        ...next,
        add: next.add.filter((id) => !enabled(id)),
        remove: next.remove.filter((id) => enabled(id)),
      };
    }
    setDraft(next);
  }

  function undo(row: Row) {
    if (!saved || !draft) return;
    if (row === "addons") change({ ...draft, add: [], remove: [] });
    else change({ ...draft, [row]: saved[row] });
  }

  async function apply() {
    if (!saved || !draft) return;
    setApplyError(null);
    setStarted(Date.now());
    try {
      const done = await request("settings-apply", {
        id,
        worktree,
        choices: choicesFor(saved, draft),
      });
      if (!alive.current) return;
      setResult(done);
      await load();
    } catch (cause) {
      if (alive.current) setApplyError(failure(cause));
    } finally {
      if (alive.current) setStarted(null);
    }
  }

  const versionSection = (
    <RepositoryRhizome
      {...rhizome}
      worktree={{
        ...rhizome.worktree,
        trusted: rhizome.worktree.trusted || trustedHere,
      }}
      unsupported={problem?.code === "setup_unsupported"}
      disabled={running || refreshing}
      onChanging={setChangingVersion}
      onReload={async () => {
        reports.current++;
        setCurrent(null);
        setPlan(null);
        setDraft(null);
        setResult(null);
        setApplyError(null);
        await load();
      }}
    />
  );

  if (!current || !draft || !plan || !saved) {
    if (!problem) {
      return (
        <>
          {versionSection}
          <p className="repo-settings-status" role="status">
            Reading this repository's settings…
          </p>
        </>
      );
    }
    if (problem.code === "trust_required") {
      return (
        <>
          {versionSection}
          <div className="repo-settings-status">
            <h2>Trust this worktree?</h2>
            <p>
              Its settings are read and changed by the Rhizome executable this worktree selects.
              Trust is recorded for this worktree only.
            </p>
            <p className="path confirmation-path">{worktree}</p>
            <button
              className="primary"
              disabled={refreshing}
              onClick={() =>
                void request("trust", { id, worktree })
                  .then(() => {
                    setTrustedHere(true);
                    return load();
                  })
                  .catch((cause) => setProblem(failure(cause)))
              }
            >
              Trust and continue
            </button>
          </div>
        </>
      );
    }
    return (
      <>
        {versionSection}
        {problem.code !== "setup_unsupported" && (
          <div className="repo-settings-status" role="alert">
            <h2>Could not read this repository's settings</h2>
            <p>{problem.message}</p>
            <button className="primary" disabled={refreshing} onClick={() => void load()}>
              Try again
            </button>
          </div>
        )}
      </>
    );
  }

  const addonOn = (addonId: string) =>
    draft.add.includes(addonId) ||
    (!draft.remove.includes(addonId) && !!plan.addons.find((a) => a.id === addonId)?.enabled);
  // Each toggle is an explicit override, since a workflow change can turn
  // default add-ons on or off; change() drops the ones that change nothing.
  const toggleAddon = (addonId: string, on: boolean) =>
    change({
      ...draft,
      add: on ? [...without(draft.add, addonId), addonId] : without(draft.add, addonId),
      remove: on ? without(draft.remove, addonId) : [...without(draft.remove, addonId), addonId],
    });
  const provider = current.search.providers.find((p) => p.id === draft.search);
  const pending = plan.writes.summary.length > 0;
  const kept = plan.writes.kept ?? [];
  const rowClass = (row: Row) => (rows.includes(row) ? "setup-row changed" : "setup-row");
  const undoButton = (row: Row, label: string) =>
    rows.includes(row) && (
      <button className="text-button undo" aria-label={`Undo ${label}`} onClick={() => undo(row)}>
        Undo
      </button>
    );

  return (
    <div className="repo-settings">
      {versionSection}
      <p className="repo-settings-lede">
        Saved in this worktree's files, so everyone who opens the repository gets the same setup.
        Commit the changes after you apply them.
      </p>
      {result && (
        <div className="repo-settings-result" role="status">
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
          {result.kept.length > 0 && (
            <p className="hint">Left as you edited them: {joinWords(result.kept)}.</p>
          )}
          {result.warnings.map((warning) => (
            <p key={warning} className="warning-text">
              {warning}
            </p>
          ))}
          {result.pin.error && (
            <p className="warning-text">
              Rhizome {result.pin.version} could not be installed for this repository:{" "}
              {result.pin.error}.
            </p>
          )}
          {result.commit.length > 0 && (
            <p>Commit {joinWords(result.commit)} so your team shares this setup.</p>
          )}
        </div>
      )}
      <fieldset
        className="setup-form"
        disabled={running || changingVersion || problem?.code === "setup_unsupported"}
      >
        <div className={rowClass("workflow")} role="radiogroup" aria-labelledby="repo-workflow">
          <h2 id="repo-workflow">Workflow {rows.includes("workflow") && <ChangedMark />}</h2>
          <div className="choices">
            {current.workflows.map((workflow) => (
              <label key={workflow.id} className="choice">
                <input
                  type="radio"
                  name="repo-workflow"
                  checked={draft.workflow === workflow.id}
                  onChange={() => change({ ...draft, workflow: workflow.id })}
                />
                <span className="choice-label">
                  {workflow.label}
                  {current.workflow === workflow.id && <span className="recommended">Current</span>}
                </span>
                <span className="choice-detail">{workflow.description}</span>
              </label>
            ))}
            {rows.includes("workflow") && (
              <p className="hint">
                Rhizome's files for the workflow you leave are removed; your own docs stay. To keep
                them for your team instead, run <code>rzm init</code> in a terminal.
              </p>
            )}
          </div>
          {undoButton("workflow", "workflow")}
        </div>

        <div className={rowClass("addons")} role="group" aria-labelledby="repo-addons">
          <h2 id="repo-addons">Add-ons {rows.includes("addons") && <ChangedMark />}</h2>
          <div className="choices">
            {plan.addons.map((addon) => (
              <label key={addon.id} className="choice">
                <input
                  type="checkbox"
                  checked={addonOn(addon.id)}
                  onChange={(e) => toggleAddon(addon.id, e.target.checked)}
                />
                <span className="choice-label">{addon.label}</span>
                <span className="choice-detail">
                  {addon.description}.
                  {addon.defaultFor.includes(draft.workflow)
                    ? " Included with this workflow by default."
                    : ""}
                </span>
              </label>
            ))}
          </div>
          {undoButton("addons", "add-ons")}
        </div>

        <div className={rowClass("agents")} role="group" aria-labelledby="repo-agents">
          <h2 id="repo-agents">Agents {rows.includes("agents") && <ChangedMark />}</h2>
          <div>
            <div className="inline-choices">
              {current.agents.map((agent) => (
                <label key={agent.id}>
                  <input
                    type="checkbox"
                    checked={draft.agents.includes(agent.id)}
                    onChange={(e) =>
                      change({
                        ...draft,
                        agents: e.target.checked
                          ? [...without(draft.agents, agent.id), agent.id]
                          : without(draft.agents, agent.id),
                      })
                    }
                  />
                  {agent.label}
                  {agent.detected && <span className="muted"> · {detected[agent.detected]}</span>}
                </label>
              ))}
            </div>
            <p className="hint">
              AGENTS.md and shared skills in .agents/ are always written; most agents read them.
            </p>
          </div>
          {undoButton("agents", "agents")}
        </div>

        <div className={rowClass("search")}>
          <h2>
            <label htmlFor="repo-search">Search</label> {rows.includes("search") && <ChangedMark />}
          </h2>
          <div>
            <div className="search-choice">
              <select
                id="repo-search"
                value={draft.search}
                onChange={(e) => change({ ...draft, search: e.target.value })}
              >
                {current.search.providers.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </select>
              <span className={provider && !provider.ready ? "warning-ink" : "muted"}>
                {draft.search === "off"
                  ? "Semantic search is off; keyword search still works."
                  : provider?.ready
                    ? provider.key
                      ? "Key found on this Mac"
                      : "Installed"
                    : provider?.key
                      ? "Needs a key on this Mac"
                      : "Not installed"}
              </span>
            </div>
            {provider && !provider.ready && provider.key && (
              <p className="hint">
                Without a key, semantic search stays off.{" "}
                <button className="link-button" onClick={onProviderSettings}>
                  Save a key in Rhizome on this Mac
                </button>
              </p>
            )}
            {provider && !provider.ready && !provider.key && provider.hint && (
              <p className="hint">{provider.hint}</p>
            )}
          </div>
          {undoButton("search", "search")}
        </div>

        <div className="setup-row">
          <h2>What gets indexed</h2>
          <dl className="findings">
            <dt>Docs</dt>
            <dd>{current.findings.docs}</dd>
            <dt>Code</dt>
            <dd>{current.findings.code}</dd>
          </dl>
          <button onClick={onEditRules}>Edit rules…</button>
        </div>
      </fieldset>

      <p className="repo-settings-foot hint">
        Other settings live in <span className="path">.rhizome/config.yml</span>.
      </p>

      {(rows.length > 0 || pending || kept.length > 0 || running || applyError) && (
        <div className="apply-bar" role="region" aria-label="Unapplied changes">
          <div className={refreshing ? "writes refreshing" : "writes"} aria-busy={refreshing}>
            <p className="apply-what">
              <ChangedMark />
              {rows.length > 0
                ? `${plural(rows.length, "change")} not applied`
                : pending
                  ? "Rhizome has updates for this repository's files"
                  : "Files you edited have newer versions"}
            </p>
            <details>
              <summary>
                {pending
                  ? `Writes ${plural(plan.writes.files.length, "file")}`
                  : rows.length > 0
                    ? "Nothing to write"
                    : plural(kept.length, "edited file")}
                {refreshing && <span className="muted"> · updating…</span>}
              </summary>
              <ul className="writes-summary">
                {plan.writes.summary.map((line) => (
                  <li key={line}>{line}</li>
                ))}
              </ul>
              {plan.writes.files.length > 0 && (
                <ul className="file-list">
                  {plan.writes.files.map((file) => (
                    <li key={file}>{file}</li>
                  ))}
                </ul>
              )}
              {kept.length > 0 && (
                <p className="hint">
                  Left as you edited them: {joinWords(kept)}. Run <code>rzm init</code> in a
                  terminal to take their updates.
                </p>
              )}
            </details>
            {applyError && (
              <p className="error-text" role="alert">
                Could not apply: {applyError.message}
              </p>
            )}
          </div>
          <div className="actions">
            {running ? (
              <span className="setup-running" role="status" aria-live="polite">
                <span className="spinner" aria-hidden="true" />
                Applying… <Elapsed started={started} />
              </span>
            ) : (
              <>
                {rows.length > 0 && (
                  <button onClick={() => change(saved)} disabled={running}>
                    Discard
                  </button>
                )}
                {(rows.length > 0 || pending) && (
                  <button
                    className="primary"
                    disabled={refreshing || changingVersion || !pending || !!problem}
                    onClick={() => void apply()}
                  >
                    {rows.length > 0 ? "Apply changes" : "Update files"}
                  </button>
                )}
              </>
            )}
          </div>
        </div>
      )}
      {problem && (
        <p className="error-text" role="alert">
          {problem.message}
        </p>
      )}
    </div>
  );
}
