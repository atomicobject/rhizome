import { useEffect, useRef, useState, type FormEvent } from "react";
import { failure, request, type Failure, type Scope, type ScopeEdits, type ScopeRule } from "./api";
import { Notice } from "./Status";

/** The root-relative path a rule names: `/testdata/` is `testdata`. */
export function rulePath(pattern: string) {
  return pattern.replace(/^!?\//, "").replace(/\\(.)/g, "$1").replace(/\/$/, "");
}

export type RuleAction = { label: string; run: () => void } | undefined;

const layers: { layer: ScopeRule["layer"]; title: string; hint?: string }[] = [
  {
    layer: "default",
    title: "Built-in list",
    hint: "Applies while .rhizome/ignore has no rules. Changing a rule writes the list to .rhizome/ignore first.",
  },
  {
    layer: "gitignore",
    title: "Inherited from .gitignore",
    hint: "Rhizome follows the .gitignore files it reads. Change them there, or index a folder they exclude.",
  },
  { layer: "rhizome", title: "Rhizome rules" },
  {
    layer: "config",
    title: "Notes excludes",
    hint: "From notes.excludes in .rhizome/config.yml. Edit that file to change them.",
  },
];

function where(rule: ScopeRule) {
  if (rule.planned) return "planned";
  if (!rule.line) return rule.source ?? "";
  return rule.layer === "rhizome" ? `line ${rule.line}` : `${rule.source}:${rule.line}`;
}

function why(rule: ScopeRule) {
  if (rule.reason) return rule.reason;
  if (rule.included) return "indexed even though an earlier rule skips it";
  if (rule.folder) return `inside ${rule.folder}/`;
  return "";
}

/** Every rule that decides what Rhizome indexes in one worktree, grouped by
 * layer in evaluation order, with the edits the caller allows. */
export function IndexScope({
  name,
  path,
  scope,
  busy,
  error,
  note,
  actionFor,
  kept,
  onSkip,
  onInclude,
  onClose,
  closeLabel,
}: {
  name: string;
  path: string;
  scope: Scope;
  busy: boolean;
  error?: Failure | null;
  /** How edits apply: held until setup, or written right away. */
  note: string;
  actionFor: (rule: ScopeRule) => RuleAction;
  kept: { path: string; undo?: () => void }[];
  onSkip: (path: string) => void;
  onInclude: (path: string) => void;
  onClose: () => void;
  closeLabel: string;
}) {
  const [entry, setEntry] = useState("");
  const value = entry.trim();
  function submit(event: FormEvent) {
    event.preventDefault();
    if (!value) return;
    onSkip(value);
    setEntry("");
  }
  return (
    <section className="scope-page" aria-labelledby="scope-title">
      <div className="section-heading">
        <div>
          <h1 id="scope-title">What gets indexed in {name}</h1>
          <p className="path">{path}</p>
        </div>
        <button onClick={onClose}>{closeLabel}</button>
      </div>
      <p className="lede">
        Rhizome indexes every file here except hidden files and folders and the paths these rules
        skip. Rules apply from top to bottom, and the last rule that matches a path wins. {note}
      </p>
      <form className="scope-add" onSubmit={submit}>
        <input
          aria-label="Folder or file"
          placeholder="Folder or file, such as web/src/generated"
          spellCheck={false}
          value={entry}
          onChange={(event) => setEntry(event.target.value)}
        />
        <button type="submit" disabled={busy || !value}>
          Skip
        </button>
        <button
          type="button"
          title="Index a folder that .gitignore excludes"
          disabled={busy || !value}
          onClick={() => {
            onInclude(value);
            setEntry("");
          }}
        >
          Index ignored folder
        </button>
      </form>
      {error && (
        <p className="error-text" role="alert">
          {error.message}
        </p>
      )}
      {layers.map(({ layer, title, hint }) => {
        const rules = scope.rules.filter((rule) => rule.layer === layer);
        if (rules.length === 0 && layer !== "gitignore" && layer !== "rhizome") return null;
        return (
          <section key={layer} className="scope-group" aria-label={title}>
            <h2>
              {title}
              {layer === "rhizome" && <span className="scope-source">.rhizome/ignore</span>}
            </h2>
            {hint && <p className="hint">{hint}</p>}
            {rules.length === 0 ? (
              <p className="scope-empty">None.</p>
            ) : (
              <table className="scope-table">
                <tbody>
                  {rules.map((rule) => {
                    const action = actionFor(rule);
                    return (
                      <tr
                        key={`${rule.source}:${rule.line}:${rule.pattern}`}
                        className={rule.planned ? "planned" : undefined}
                      >
                        <td className="pattern">{rule.pattern}</td>
                        <td className="why">{why(rule)}</td>
                        <td className="where">{where(rule)}</td>
                        <td className="act">
                          {action && (
                            <button
                              className="row-action"
                              disabled={busy}
                              aria-label={`${action.label}: ${rule.pattern}`}
                              onClick={action.run}
                            >
                              {action.label}
                            </button>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
            {layer === "rhizome" && kept.length > 0 && (
              <p className="scope-kept">
                <span className="hint">Kept indexed, so setup never suggests skipping them:</span>{" "}
                {kept.map((item) => (
                  <span key={item.path} className="kept-path">
                    <code>{item.path}</code>
                    {item.undo && (
                      <button
                        className="row-action"
                        aria-label={`Undo: ${item.path}`}
                        disabled={busy}
                        onClick={item.undo}
                      >
                        Undo
                      </button>
                    )}
                  </span>
                ))}
              </p>
            )}
          </section>
        );
      })}
    </section>
  );
}

/** The page for a configured worktree: each edit is written through the
 * worktree's own Rhizome, which then reports the rules again. */
export function ScopePage({
  repository,
  worktree,
  name,
  onClose,
  onManageInstallation,
}: {
  repository: string;
  worktree: string;
  name: string;
  onClose: () => void;
  onManageInstallation: () => void;
}) {
  const [scope, setScope] = useState<Scope | null>(null);
  const [problem, setProblem] = useState<Failure | null>(null);
  const [busy, setBusy] = useState(false);
  const alive = useRef(true);

  async function run(task: () => Promise<Scope>) {
    setBusy(true);
    try {
      const next = await task();
      if (!alive.current) return;
      setScope(next);
      setProblem(null);
    } catch (cause) {
      if (alive.current) setProblem(failure(cause));
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  const load = () => run(() => request("scope", { id: repository, worktree }));
  const edit = (edits: Partial<ScopeEdits>) =>
    run(() =>
      request("scope-edit", {
        id: repository,
        worktree,
        edits: { skip: [], removeRules: [], includeIgnored: [], ...edits },
      }),
    );

  useEffect(() => {
    alive.current = true;
    void load();
    return () => {
      alive.current = false;
    };
    // Loads once per worktree; the caller keys this page by worktree.
  }, []);

  if (!scope) {
    if (!problem) return <Notice title="Reading the rules…" />;
    return (
      <Notice title="Could not read what gets indexed" failure={problem}>
        <p className="path">{worktree}</p>
        <div className="actions">
          <button onClick={onClose}>Close</button>
          {["setup_unsupported", "missing_executable", "install_error"].includes(problem.code) && (
            <button onClick={onManageInstallation}>Manage installation</button>
          )}
          {problem.code === "trust_required" && (
            <button
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  await request("trust", { id: repository, worktree });
                  return request("scope", { id: repository, worktree });
                })
              }
            >
              Trust and continue
            </button>
          )}
          <button className="primary" disabled={busy} onClick={() => void load()}>
            Try again
          </button>
        </div>
      </Notice>
    );
  }
  return (
    <IndexScope
      name={name}
      path={worktree}
      scope={scope}
      busy={busy}
      error={problem}
      note="Each change is written to .rhizome/ignore right away, and a running Rhizome picks it up."
      actionFor={(rule) =>
        rule.layer === "rhizome" || rule.layer === "default"
          ? {
              label: rule.reason ? "Index again" : "Remove",
              run: () => void edit({ removeRules: [rule.pattern] }),
            }
          : undefined
      }
      kept={scope.keepIndexed.map((path) => ({ path }))}
      onSkip={(path) => void edit({ skip: [path] })}
      onInclude={(path) => void edit({ includeIgnored: [path] })}
      onClose={onClose}
      closeLabel="Done"
    />
  );
}
