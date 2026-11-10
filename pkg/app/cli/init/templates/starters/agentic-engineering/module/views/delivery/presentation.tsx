import type { ReactNode } from "react";
import { useEffect, useState, useSyncExternalStore } from "react";
import {
  DisclosureIcon,
  Facts,
  Fact,
  RecordChip,
  Section,
  StatusMark,
  typeLabel,
  useMarkReady,
  usePageNavigation,
  useTypeDocs,
  useViewActive,
  workspaceViewHref,
} from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import type { SectionStatus } from "@rhizome/kit";
import { groups, groupResult, identity } from "./lib/index.ts";
import type {
  Coverage,
  Effort,
  Portfolio,
  Ref,
  RecordSummary,
  Spec,
  Story,
  Stage,
  Prompt,
  Row,
} from "./lib/index.ts";
import "./delivery.css";

export type PortfolioProps = { portfolio: Portfolio; status: SectionStatus };

export function subjectPortfolio(record: Spec | Effort): Portfolio {
  return {
    specs: record.kind === "spec" ? [record] : [],
    efforts: record.kind === "effort" ? [record] : [],
    specCoverage: record.coverage,
    effortCoverage: record.coverage,
  };
}

export function RecordWorkflow({ record }: { record: Spec | Effort }) {
  return (
    <div className="ae-record-workflow">
      <WorkflowMap portfolio={subjectPortfolio(record)} subject={record.ref} showCounts={false} />
    </div>
  );
}

export function Ready({ status }: { status: SectionStatus }) {
  const mark = useMarkReady();
  useEffect(() => {
    if (status.status !== "loading") mark();
  }, [status.status, mark]);
  return null;
}

export function RecordLink({ record }: { record: RecordSummary }) {
  const nav = usePageNavigation();
  return (
    <RecordChip
      recordKey={identity(record.ref)}
      wrap
      onOpen={() => nav.openNode(record.ref)}
      title={record.locator ?? record.ref.notePath}
    >
      {record.title || record.ref.notePath}
    </RecordChip>
  );
}

export function StageLink({
  stage,
  current = false,
  children,
}: {
  stage: Stage;
  current?: boolean;
  children: ReactNode;
}) {
  const nav = usePageNavigation();
  return (
    <a
      className="ae-link"
      aria-current={current ? "page" : undefined}
      href={workspaceViewHref("agentic-engineering.stage", { kind: "standalone" }, { stage })}
      onClick={(event) => {
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        nav.openView("agentic-engineering.stage", { kind: "standalone" }, { stage });
      }}
    >
      {children}
    </a>
  );
}

const singular = {
  specs: "spec",
  stories: "story",
  efforts: "effort",
  observations: "observation",
  criteria: "criterion",
  records: "record",
  visits: "visit",
} as const;

export function unit(count: number, plural: keyof typeof singular) {
  return count === 1 ? singular[plural] : plural;
}

export function sourceLocator(ref: Ref) {
  return `${ref.notePath}${ref.fragment ? `#${ref.fragment}` : ""}`;
}

/** The recorded status word with the mark the house gives that word. Nothing is inferred. */
export function RecordedStatus({ status }: { status: string | null | undefined }) {
  return status ? <StatusMark value={status} values={[]} /> : <>Status unavailable</>;
}

/** A type's human label, with its identifier in the tooltip and as the fallback evidence. */
export function TypeName({ name }: { name: string }) {
  const doc = useTypeDocs([name]).docs[name];
  return doc ? <span title={name}>{typeLabel(doc)}</span> : <code className="ae-path">{name}</code>;
}

type NoticeTone = "neutral" | "risk";

/** The house status shapes: a neutral ring, or the risk disc with its exclamation cut out. */
export function NoticeMark({ tone = "neutral" }: { tone?: NoticeTone }) {
  return tone === "risk" ? (
    <svg
      className="ae-notice-mark"
      data-tone="risk"
      viewBox="0 0 12 12"
      width="11"
      height="11"
      aria-hidden="true"
    >
      <path
        fill="currentColor"
        fillRule="evenodd"
        d="M6 0a6 6 0 1 0 0 12A6 6 0 0 0 6 0ZM5.25 2.5h1.5v4.75h-1.5ZM5.25 8.25h1.5v1.5h-1.5Z"
      />
    </svg>
  ) : (
    <svg className="ae-notice-mark" viewBox="0 0 9 9" width="9" height="9" aria-hidden="true">
      <circle cx="4.5" cy="4.5" r="3.75" fill="none" stroke="currentColor" strokeWidth="1.5" />
    </svg>
  );
}

/** One treatment for every failed, partial, and changed state. Live-region roles stay on the lines inside unless `role` names the whole notice. */
export function Notice({
  tone = "neutral",
  role,
  children,
}: {
  tone?: NoticeTone;
  role?: "status" | "alert";
  children: ReactNode;
}) {
  return (
    <div className="ae-notice" role={role}>
      <NoticeMark tone={tone} />
      <div className="ae-notice-body">{children}</div>
    </div>
  );
}

export function ErrorDetail({ error }: { error: Error }) {
  return (
    <details>
      <summary>
        <DisclosureIcon open={false} />
        Error detail
      </summary>
      <code className="ae-path">{error.message}</code>
    </details>
  );
}

// ponytail: the host gives a panel the active page's subject but not its params, so the stage
// page names its stage for the panel through this window's session storage. Read the stage from
// the panel's location instead once a panel invocation carries the page's params.
const stageKey = "agentic-engineering.current-stage";

function announcedStage() {
  try {
    return sessionStorage.getItem(stageKey);
  } catch {
    return null;
  }
}

function onStageAnnounced(listener: () => void) {
  window.addEventListener("storage", listener);
  return () => window.removeEventListener("storage", listener);
}

/** The stage page on screen names its stage, and withdraws it when it leaves. */
export function useAnnounceStage(stage: Stage | null) {
  const active = useViewActive();
  useEffect(() => {
    if (!active || !stage) return;
    try {
      sessionStorage.setItem(stageKey, stage);
    } catch {
      // The panel shows no current stage when browser storage is unavailable.
      return;
    }
    return () => {
      if (announcedStage() === stage) sessionStorage.removeItem(stageKey);
    };
  }, [active, stage]);
}

export function useAnnouncedStage() {
  return useSyncExternalStore(onStageAnnounced, announcedStage);
}

export function parentSpecSummary(result: ReturnType<typeof groupResult>) {
  if (result.state !== "records" || result.parentSpecCount === null) return "";
  return ` in ${result.complete ? "" : "at least "}${result.parentSpecCount} ${unit(result.parentSpecCount, "specs")}`;
}

export function WorkflowMap({
  portfolio,
  current,
  subject,
  showCounts = true,
  compact = false,
}: {
  portfolio: Portfolio;
  current?: Stage;
  subject?: Ref;
  showCounts?: boolean;
  compact?: boolean;
}) {
  return (
    <nav className={`ae-map${compact ? " ae-map-compact" : ""}`} aria-label="Delivery workflow">
      {(["Shape", "Build", "Settle"] as const).map((activity) => (
        <div key={activity} className="ae-activity" data-activity={activity}>
          <strong>{activity}</strong>
          <div className="ae-stages">
            {groups
              .filter((group) => group.primary && group.activity === activity)
              .map((group) => {
                const result = groupResult(group.stage, portfolio);
                if (result.state !== "records") return null;
                const contains =
                  subject &&
                  result.rows.some(
                    (row) =>
                      identity(row.record.ref) === identity(subject) ||
                      (row.kind === "story" && identity(row.parent.ref) === identity(subject)),
                  );
                return (
                  <div key={group.stage} className="ae-stage" data-subject={contains || undefined}>
                    <StageLink stage={group.stage} current={current === group.stage}>
                      {showCounts && (
                        <span className="ae-stage-count">
                          <span className="ae-number">
                            {result.complete ? "" : "at least "}
                            {result.count}
                          </span>{" "}
                          {unit(result.count, group.unit)}
                          {parentSpecSummary(result)}
                        </span>
                      )}{" "}
                      <span className="ae-stage-title">{group.title}</span>{" "}
                      {contains && <span className="ae-this-record">This record</span>}
                    </StageLink>
                  </div>
                );
              })}
          </div>
        </div>
      ))}
    </nav>
  );
}

export function CoverageNotice({ portfolio }: { portfolio: Portfolio }) {
  const reads = [portfolio.specCoverage, portfolio.effortCoverage];
  const errors = [...new Set(reads.flatMap((read) => read.errors))];
  const partial = reads.some((read) => !read.complete || read.truncated);
  if (!partial && !errors.length) return null;
  return (
    <Notice tone={errors.length ? "risk" : "neutral"}>
      {partial && (
        <p role="status">Partial inventory. Counts describe loaded records and may omit work.</p>
      )}
      {errors.map((error) => (
        <p role="alert" className="ae-path" key={error}>
          {error}
        </p>
      ))}
    </Notice>
  );
}

export function RowFacts({ row }: { row: Row }) {
  const record = row.record;
  return (
    <Facts note={<RecordedStatus status={record.status} />}>
      {row.kind === "spec" && (
        <Fact value={row.record.stories.length}>
          {row.record.storiesCoverage.complete
            ? unit(row.record.stories.length, "stories")
            : "loaded stories"}
        </Fact>
      )}
      {row.kind === "story" && (
        <>
          <Fact>
            {row.parent.status === "proposed"
              ? "Proposed parent contract"
              : "Active parent contract"}
          </Fact>
          <Fact value={row.record.criteria.length}>
            {row.record.criteriaCoverage.complete
              ? unit(row.record.criteria.length, "criteria")
              : "loaded criteria"}
          </Fact>
        </>
      )}
      {(row.kind === "effort" || row.kind === "review") && (
        <Fact>
          {row.record.checklist.state === "recorded" || row.record.checklist.state === "empty"
            ? `${row.record.checklist.checked} of ${row.record.checklist.total} checklist items checked`
            : row.record.checklist.state === "deferred"
              ? "Open brief for checklist evidence"
              : `Checklist ${row.record.checklist.state}`}
        </Fact>
      )}
      {row.kind === "review" && (
        <Fact>
          {row.observation === "closure-checklist"
            ? "Recorded checklist fully checked; effort remains active"
            : "Selected contract still records pending stories or criteria"}
        </Fact>
      )}
    </Facts>
  );
}

export function PromptBox({ prompt }: { prompt: Prompt }) {
  const [result, setResult] = useState("");
  useEffect(() => setResult(""), [prompt.text]);
  async function copy() {
    try {
      await navigator.clipboard.writeText(prompt.text);
      setResult("Prompt copied.");
    } catch {
      setResult("Could not copy. Select the preview text and copy it manually.");
    }
  }
  return (
    <Section
      title="Workflow prompt"
      caption="Copy this prompt into your coding agent. It follows the installed workflow and preserves selected scope."
    >
      <div className="ae-prompt">
        <textarea
          readOnly
          aria-label="Workflow prompt preview"
          value={prompt.text || prompt.reasons.join(". ")}
          rows={12}
        />
        <Button disabled={!prompt.copyable} onClick={copy}>
          Copy workflow prompt
        </Button>
        <p role="status">{result}</p>
      </div>
    </Section>
  );
}

export function EmptyGroup({ stage }: { stage: Stage }) {
  return (
    <p className="ae-note">
      {stage === "planned"
        ? "No loaded effort has recorded status planned. Select ready stories to propose a slice, then use the effort and plan workflow to record scope and a plan. Approval is established from the full recorded context."
        : "No matching records in the loaded inventory."}
    </p>
  );
}

export function ReadState({
  query,
}: {
  query: {
    state: string;
    error: Error | null;
    retained: boolean;
    refresh: () => unknown;
    data?: { coverage: Coverage } | null;
  };
}) {
  const mark = useMarkReady();
  useEffect(() => {
    if (query.state !== "loading") mark();
  }, [query.state, mark]);
  const errors = query.data?.coverage.errors ?? [];
  const partial = query.state === "partial";
  return (
    <div className="ae-read-state">
      {query.state === "loading" && (
        <p role="status" className="ae-note">
          Loading delivery records…
        </p>
      )}
      {(query.error || partial || errors.length > 0) && (
        <Notice tone={query.error ? "risk" : "neutral"}>
          {query.error && (
            <p role="alert">
              Read failed. <Button onClick={() => query.refresh()}>Retry read</Button>
            </p>
          )}
          {query.retained && (
            <p role="status">Showing previous saved records after a failed refresh.</p>
          )}
          {partial && (
            <p role="status">
              Some source evidence is unavailable.{" "}
              <Button className="ae-refresh" onClick={() => query.refresh()}>
                Refresh records
              </Button>
            </p>
          )}
          {errors.map((error, index) => (
            <p role="status" className="ae-path" key={index}>
              {error}
            </p>
          ))}
          {query.error && <ErrorDetail error={query.error} />}
        </Notice>
      )}
      <p className="ae-note ae-saved">
        Saved records. Unsaved workspace edits are not included.{" "}
        {query.state === "ready" && (
          <Button className="ae-refresh" onClick={() => query.refresh()}>
            Refresh records
          </Button>
        )}
      </p>
    </div>
  );
}

const specViews: Record<string, string> = {
  ProcessSpec: "agentic-engineering.spec.process",
  ProductSpec: "agentic-engineering.spec.product",
  TechnicalSpec: "agentic-engineering.spec.technical",
  ExperienceSpec: "agentic-engineering.spec.experience",
  OperationsSpec: "agentic-engineering.spec.operations",
};
export function StoryLink({ story, parent }: { story: Story; parent: Spec }) {
  const nav = usePageNavigation();
  const type = parent.ref.typeName;
  return (
    <RecordChip
      recordKey={identity(story.ref)}
      wrap
      onOpen={() => {
        if (type && specViews[type])
          nav.openView(
            specViews[type],
            { kind: "node", type, ref: parent.ref },
            { story: story.locator || identity(story.ref) },
          );
        else nav.openNode(parent.ref);
      }}
    >
      {story.title}
    </RecordChip>
  );
}
