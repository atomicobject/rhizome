import type { ReactNode } from "react";
import type { ValidationActionSnapshot, ValidationDiagnostic } from "../../api/types";
import type { KindRow } from "./IssueKindList";
import { diagnosticNoteTarget } from "./issueNavigation";
import {
  checkLabel,
  issueHelp,
  issueLabel,
  issueSummary,
  safetyLabel,
  type IssueHelp,
} from "./issueLabels";

/** Issues that share a file, in server file order. */
export type FileGroup = {
  /** The server's file key: primary path, else first affected path; "" is vault-wide. */
  key: string;
  /** Issues in this file across all pages. */
  total: number;
  diagnostics: ValidationDiagnostic[];
};

export function fileGroups(
  diagnostics: ValidationDiagnostic[],
  totals: ReadonlyMap<string, number>,
): FileGroup[] {
  const groups = new Map<string, ValidationDiagnostic[]>();

  for (const diagnostic of diagnostics) {
    const key = diagnostic.primaryPath || diagnostic.affectedPaths?.[0] || "";
    groups.set(key, [...(groups.get(key) ?? []), diagnostic]);
  }

  return [...groups].map(([key, items]) => ({
    key,
    total: totals.get(key) ?? items.length,
    diagnostics: items,
  }));
}

type RowProps = {
  selectedKey: string | null;
  /** The one issue row in the Tab order; arrow keys move between rows. */
  tabbableKey: string | undefined;
  onSelect: (diagnostic: ValidationDiagnostic) => void;
};

export function FileGroupSection({ group, ...rowProps }: RowProps & { group: FileGroup }) {
  const path = diagnosticPath(group.diagnostics[0]);

  if (group.diagnostics.length === 1) {
    const [diagnostic] = group.diagnostics;

    return (
      <IssueRow diagnostic={diagnostic} {...rowProps}>
        <PathLabel path={path} />
        <span className="problems-row__kind">{issueLabel(diagnostic.code)}</span>
      </IssueRow>
    );
  }

  return (
    <details className="problems-group" open>
      <summary>
        <PathLabel path={path} />
        <span className="problems-kind__count">{group.total}</span>
      </summary>
      <div className="problems-group__items">
        {group.diagnostics.map((diagnostic) => (
          <IssueRow key={diagnostic.issueKey} diagnostic={diagnostic} {...rowProps}>
            <span className="problems-row__kind">{issueLabel(diagnostic.code)}</span>
          </IssueRow>
        ))}
      </div>
    </details>
  );
}

/** Column 2's heading: the selected kind or variant, what it means, and bulk actions. */
export function FindingsHeader({
  row,
  fixableActionIds,
  onSelectActions,
  children,
}: {
  row: KindRow;
  /** Browser-applicable repairs of the loaded findings that are not selected yet. */
  fixableActionIds: string[];
  onSelectActions: (ids: string[]) => void;
  children?: ReactNode;
}) {
  const help = row.selection ? issueHelp(row.code) : null;
  const fixCount = fixableActionIds.length;

  return (
    <header className="problems-findings__head">
      <div className="problems-findings__title">
        <strong>{row.kindLabel}</strong>
        {row.variantLabel && <span className="problems-findings__variant">{row.variantLabel}</span>}
        {row.check && <span className="problems-kind__check">{checkLabel(row.check)}</span>}
        <span className="problems-kind__count">{row.count}</span>
      </div>
      {(help || fixCount > 0) && (
        <div className="problems-findings__about">
          {help && <IssueHelpText text={help.meaning} />}
          {fixCount > 0 && (
            <button type="button" onClick={() => onSelectActions(fixableActionIds)}>
              Select {fixCount} {fixCount === 1 ? "repair" : "repairs"}
            </button>
          )}
        </div>
      )}
      {children}
    </header>
  );
}

export function IssueRow({
  diagnostic,
  selectedKey,
  tabbableKey,
  onSelect,
  children,
}: RowProps & { diagnostic: ValidationDiagnostic; children: ReactNode }) {
  const summary = issueSummary(diagnostic);
  const line = diagnostic.location?.unit === "line" ? diagnostic.location.start : null;
  const selected = diagnostic.issueKey === selectedKey;

  return (
    <button
      type="button"
      className={`problems-row${selected ? " is-selected" : ""}`}
      aria-current={selected ? "true" : undefined}
      tabIndex={diagnostic.issueKey === tabbableKey ? 0 : -1}
      data-issue-key={diagnostic.issueKey}
      title={diagnostic.message}
      onClick={() => onSelect(diagnostic)}
    >
      {children}
      {summary && <span className="problems-row__summary">{summary}</span>}
      {line !== null && <span className="problems-row__line">L{line}</span>}
    </button>
  );
}

export function PathLabel({ path }: { path: string }) {
  const slash = path.lastIndexOf("/");

  return (
    <span className="problems-row__path">
      {slash >= 0 && <span className="problems-row__dir">{path.slice(0, slash + 1)}</span>}
      <span className="problems-row__file">{path.slice(slash + 1)}</span>
    </span>
  );
}

export function IssueHelpText({ text }: { text: string }) {
  return (
    <span className="issue-help-text">
      {text.split("`").map((part, index) =>
        // Odd segments sit between backticks.
        index % 2 ? <code key={index}>{part}</code> : part,
      )}
    </span>
  );
}

export function diagnosticPath(diagnostic: ValidationDiagnostic) {
  return diagnostic.primaryPath || diagnostic.affectedPaths?.[0] || "Vault-wide";
}

export function DiagnosticDetail({
  diagnostic,
  actions,
  actionsPending = false,
  selectedActions,
  onToggleAction,
  onOpenNote,
  onBack,
  position,
  total,
  onPrevious,
  onNext,
}: {
  diagnostic: ValidationDiagnostic;
  actions: ValidationActionSnapshot[];
  actionsPending?: boolean;
  selectedActions: Set<string>;
  onToggleAction: (id: string) => void;
  onOpenNote: (path: string) => void;
  onBack: () => void;
  position: number;
  total: number;
  onPrevious: () => void;
  onNext: () => void;
}) {
  const target = diagnosticNoteTarget(diagnostic);
  const path = diagnostic.primaryPath || diagnostic.affectedPaths?.[0];
  const help = issueHelp(diagnostic.code);

  return (
    <article
      className="problem-detail"
      aria-label="Issue detail"
      onKeyDown={(event) => {
        if (event.altKey && event.key === "ArrowUp") onPrevious();

        if (event.altKey && event.key === "ArrowDown") onNext();

        if (event.key === "Escape") onBack();
      }}
    >
      <div className="problem-detail__bar">
        <button type="button" className="problem-detail__back" onClick={onBack}>
          ← Issue list
        </button>
        <span className="problem-detail__check">{checkLabel(diagnostic.check)}</span>
        <nav className="problem-detail__navigation" aria-label="Issue navigation">
          <button
            type="button"
            onClick={onPrevious}
            disabled={position <= 0}
            aria-label="Previous issue"
            title="Previous issue (Alt+↑)"
          >
            ‹
          </button>
          <span>
            {position + 1} / {total}
          </span>
          <button
            type="button"
            onClick={onNext}
            disabled={position >= total - 1}
            aria-label="Next issue"
            title="Next issue (Alt+↓)"
          >
            ›
          </button>
        </nav>
      </div>
      <h3>{issueLabel(diagnostic.code)}</h3>
      <p className="problem-detail__message">
        {diagnostic.message || "This item does not satisfy the selected validation check."}
      </p>
      {target && path && (
        <button type="button" className="problem-detail__path" onClick={() => onOpenNote(target)}>
          <code>{path}</code>
          <span>{target.includes("#issue:") ? "Open at issue →" : "Open note →"}</span>
        </button>
      )}
      <dl className="problem-detail__facts">
        {diagnostic.field && (
          <Fact label="Field">
            <code>{diagnostic.field}</code>
          </Fact>
        )}
        {diagnostic.variant && <Fact label="Case">{diagnostic.variant.label}</Fact>}
        {diagnostic.target && (
          <Fact label="Target">
            <code>{diagnostic.target}</code>
          </Fact>
        )}
        {diagnostic.source && diagnostic.source !== path && (
          <Fact label="Source">{diagnostic.source}</Fact>
        )}
        {diagnostic.location && <Fact label="Location">{formatLocation(diagnostic.location)}</Fact>}
        {(diagnostic.affectedPaths?.length ?? 0) > 1 && (
          <Fact label="Affected files">{diagnostic.affectedPaths?.join(", ")}</Fact>
        )}
      </dl>
      {help && <HelpSection help={help} />}
      <section className="problem-detail__actions" aria-label="Available repairs">
        <h4>Resolution</h4>
        {actionsPending ? (
          <p role="status">
            Updating this finding. Repair actions will appear when current results load.
          </p>
        ) : actions.length ? (
          actions.map((action) => (
            <RepairAction
              key={action.id}
              action={action}
              selected={selectedActions.has(action.id)}
              onToggle={onToggleAction}
            />
          ))
        ) : (
          <p>No automatic repair is available. Open the note to resolve this issue manually.</p>
        )}
      </section>
    </article>
  );
}

function HelpSection({ help }: { help: IssueHelp }) {
  return (
    <dl className="problem-detail__help">
      <div>
        <dt>What it means</dt>
        <dd>
          <IssueHelpText text={help.meaning} />
        </dd>
      </div>
      <div>
        <dt>How to fix</dt>
        <dd>
          <IssueHelpText text={help.fix} />
        </dd>
      </div>
    </dl>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

function RepairAction({
  action,
  selected,
  onToggle,
}: {
  action: ValidationActionSnapshot;
  selected: boolean;
  onToggle: (id: string) => void;
}) {
  const instanceCount = action.instanceCount ?? action.issueKeys?.length ?? 1;
  const fileCount = action.affectedPaths?.length ?? 0;
  const [summary, command] = splitNextCommand(action.summary);

  const description = (
    <span>
      <strong>{action.title}</strong>
      <small>
        {safetyLabel(action.safety)} · {instanceCount} {instanceCount === 1 ? "issue" : "issues"}
        {fileCount > 0 ? ` · ${fileCount} ${fileCount === 1 ? "file" : "files"}` : ""}
      </small>
      {summary && <span>{summary}</span>}
      {command && <code className="problem-action__command">{command}</code>}
    </span>
  );

  if (action.safety === "agent_required") {
    return (
      <div className="problem-action is-guidance" role="note">
        {description}
      </div>
    );
  }

  return (
    <label className="problem-action">
      <input type="checkbox" checked={selected} onChange={() => onToggle(action.id)} />
      {description}
    </label>
  );
}

/** Agent guidance ends with "Next: <command>"; show the command as code. */
function splitNextCommand(summary?: string): [string, string | null] {
  const match = /^(.*?)\s*Next:\s*(rzm .+)$/s.exec(summary ?? "");

  return match ? [match[1], match[2]] : [summary ?? "", null];
}

function formatLocation(location: NonNullable<ValidationDiagnostic["location"]>) {
  if (location.unit === "line")
    return `Line ${location.start}${location.end > location.start ? `–${location.end}` : ""}`;

  return `Bytes ${location.start}–${location.end}`;
}
