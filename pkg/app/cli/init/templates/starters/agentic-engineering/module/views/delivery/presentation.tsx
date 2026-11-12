import type { ReactNode } from "react";
import { useEffect, useState } from "react";
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

/** The groups this record belongs to, as links. Groups overlap, so a spec may name several. */
export function RecordMemberships({ record }: { record: Spec | Effort }) {
  const portfolio = subjectPortfolio(record);
  const member = groups.filter((group) => {
    const result = groupResult(group.stage, portfolio);
    return (
      result.state === "records" &&
      result.rows.some(
        (row) =>
          identity(row.record.ref) === identity(record.ref) ||
          (row.kind === "story" && identity(row.parent.ref) === identity(record.ref)),
      )
    );
  });
  if (!member.length) return null;
  return (
    <span className="ae-memberships">
      In{" "}
      {member.map((group, index) => (
        <span key={group.stage}>
          {index > 0 && ", "}
          <StageLink stage={group.stage}>{group.title}</StageLink>
        </span>
      ))}
    </span>
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

export function RecordedStatus({ status }: { status: string | null | undefined }) {
  return status ? <StatusMark value={status} values={[]} /> : <>Status unavailable</>;
}

export function TypeName({ name }: { name: string }) {
  const doc = useTypeDocs([name]).docs[name];
  return doc ? <span title={name}>{typeLabel(doc)}</span> : <code className="ae-path">{name}</code>;
}

type NoticeTone = "neutral" | "risk";

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

export function ErrorDetail({ error }: { error: { message: string } }) {
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
                      {showCounts && compact && (
                        <span
                          className="ae-stage-count"
                          aria-label={`${result.complete ? "" : "at least "}${result.count} ${unit(result.count, group.unit)}${parentSpecSummary(result)}`}
                        >
                          {result.complete ? "" : "≥"}
                          {result.count}
                        </span>
                      )}
                      {showCounts && !compact && (
                        <span
                          className="ae-number"
                          aria-label={`${result.complete ? "" : "at least "}${result.count} ${unit(result.count, group.unit)}${parentSpecSummary(result)}`}
                        >
                          {result.complete ? "" : "≥"}
                          {result.count}
                        </span>
                      )}{" "}
                      <span className="ae-stage-title">
                        {group.title}
                        {showCounts && !compact && result.parentSpecCount !== null && (
                          <span className="ae-note">
                            {" "}
                            · {result.parentSpecCount} {unit(result.parentSpecCount, "specs")}
                          </span>
                        )}
                      </span>{" "}
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

const tabLabel: Record<Stage, string> = {
  shaping: "Shaping",
  ready: "Ready",
  planned: "Planned",
  active: "Active",
  history: "History",
  contracts: "Contracts",
  review: "Review",
};

/** The stage page's own map: one row of groups with bare counts, the current one underlined. */
export function StageTabs({ portfolio, current }: { portfolio: Portfolio; current: Stage }) {
  return (
    <nav className="ae-tabs" aria-label="Delivery workflow">
      {groups.map((group) => {
        const result = groupResult(group.stage, portfolio);
        if (result.state !== "records") return null;
        return (
          <span key={group.stage} className="ae-tab" data-supporting={!group.primary || undefined}>
            <StageLink stage={group.stage} current={current === group.stage}>
              {tabLabel[group.stage]}
              {group.primary && (
                <span
                  className="ae-count"
                  aria-label={`, ${result.complete ? "" : "at least "}${result.count} ${unit(result.count, group.unit)}${parentSpecSummary(result)}`}
                >
                  {" "}
                  {result.complete ? "" : "≥"}
                  {result.count}
                </span>
              )}
            </StageLink>
          </span>
        );
      })}
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
      {partial && <p role="status">Some records didn't load. Counts may be low.</p>}
      {errors.length > 0 && <ErrorDetail error={{ message: errors.join("\n") }} />}
    </Notice>
  );
}

export function RowFacts({ row, hideStatus = false }: { row: Row; hideStatus?: boolean }) {
  const record = row.record;
  return (
    <Facts note={hideStatus ? undefined : <RecordedStatus status={record.status} />}>
      {row.kind === "spec" && (
        <Fact value={row.record.stories.length}>{unit(row.record.stories.length, "stories")}</Fact>
      )}
      {row.kind === "story" && (
        <Fact value={row.record.criteria.length}>
          {unit(row.record.criteria.length, "criteria")}
        </Fact>
      )}
      {(row.kind === "effort" || row.kind === "review") &&
        row.record.checklist.state !== "deferred" && (
          <Fact>
            {row.record.checklist.state === "recorded" || row.record.checklist.state === "empty"
              ? `${row.record.checklist.checked} of ${row.record.checklist.total} checked`
              : `Checklist ${row.record.checklist.state}`}
          </Fact>
        )}
    </Facts>
  );
}

export function PromptBox({
  prompt,
  title = "Workflow prompt",
}: {
  prompt: Prompt;
  title?: string;
}) {
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
  const reasons = [...new Set(prompt.reasons)];
  return (
    <Section title={title}>
      <div className="ae-prompt" data-empty={prompt.text ? undefined : true}>
        {prompt.text ? (
          <textarea
            readOnly
            aria-label="Workflow prompt preview"
            value={prompt.text}
            rows={Math.min(24, Math.max(6, Math.ceil(prompt.text.length / 44)))}
          />
        ) : (
          <ul className="ae-prompt-reasons" aria-label="Prompt unavailable">
            {reasons.map((reason) => (
              <li key={reason}>{reason}</li>
            ))}
          </ul>
        )}
        <div className="ae-prompt-actions">
          <Button disabled={!prompt.copyable} onClick={copy}>
            Copy workflow prompt
          </Button>
          <p role="status" className="ae-note">
            {result}
          </p>
        </div>
      </div>
    </Section>
  );
}

export function EmptyGroup({ stage }: { stage: Stage }) {
  return (
    <p className="ae-note">
      {stage === "planned" ? (
        <>
          No planned efforts. Select <StageLink stage="ready">ready stories</StageLink> to start an
          effort.
        </>
      ) : (
        "No matching records in the loaded inventory."
      )}
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
      {(query.error || query.retained) && (
        <Notice tone={query.error ? "risk" : "neutral"}>
          {query.error && (
            <p role="alert">
              Read failed. <Button onClick={() => query.refresh()}>Retry read</Button>
            </p>
          )}
          {query.retained && (
            <p role="status">Showing previous saved records after a failed refresh.</p>
          )}

          {query.error && <ErrorDetail error={query.error} />}
        </Notice>
      )}
      {(query.state === "ready" || partial) && (
        <div className="ae-saved">
          {partial && (
            <>
              <NoticeMark />
              <span role="status" className="ae-note">
                Some sources didn't load
              </span>
              {errors.length > 0 && (
                <details className="ae-partial-detail">
                  <summary>Error detail</summary>
                  <code className="ae-path">{[...new Set(errors)].join("\n")}</code>
                </details>
              )}
            </>
          )}
          <button
            className="ae-link ae-refresh"
            aria-label="Refresh records"
            title="Reload saved records. Unsaved edits are not included."
            onClick={() => query.refresh()}
          >
            Refresh
          </button>
        </div>
      )}
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
