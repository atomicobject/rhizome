import type { ReactNode } from "react";
import { useEffect, useState } from "react";
import {
  Facts,
  Fact,
  RecordChip,
  Section,
  useMarkReady,
  usePageNavigation,
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

export function StageLink({ stage, children }: { stage: Stage; children: ReactNode }) {
  const nav = usePageNavigation();
  return (
    <a
      className="ae-link"
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

export function unit(count: number, plural: "specs" | "stories" | "efforts" | "observations") {
  return count === 1
    ? { specs: "spec", stories: "story", efforts: "effort", observations: "observation" }[plural]
    : plural;
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
                  <div
                    key={group.stage}
                    className="ae-stage"
                    aria-current={current === group.stage ? "page" : undefined}
                    data-subject={contains || undefined}
                  >
                    <StageLink stage={group.stage}>
                      {showCounts && (
                        <>
                          <span className="ae-number">
                            {result.complete ? "" : "at least "}
                            {result.count}
                          </span>{" "}
                          {unit(result.count, group.unit)}
                          {parentSpecSummary(result)}
                          <br />{" "}
                        </>
                      )}
                      {group.title}
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
  return (
    <>
      {reads.some((read) => !read.complete || read.truncated) && (
        <p role="status" className="ae-note">
          Partial inventory. Counts describe loaded records and may omit work.
        </p>
      )}
      {errors.map((error) => (
        <p role="alert" key={error}>
          {error}
        </p>
      ))}
    </>
  );
}

export function RowFacts({ row }: { row: Row }) {
  const record = row.record;
  return (
    <Facts note={record.status ?? "Status unavailable"}>
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
              ? row.record.criteria.length === 1
                ? "criterion"
                : "criteria"
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
  return (
    <div className="ae-read-state">
      {query.state === "loading" && <p role="status">Loading delivery records…</p>}
      {query.error && (
        <p role="alert">
          Read failed. {query.error.message}{" "}
          <Button onClick={() => query.refresh()}>Retry read</Button>
        </p>
      )}
      {query.retained && (
        <p role="status">Showing previous saved records after a failed refresh.</p>
      )}
      {query.state === "partial" && (
        <p role="status">
          Some source evidence is unavailable.{" "}
          <Button className="ae-refresh" onClick={() => query.refresh()}>
            Refresh records
          </Button>
        </p>
      )}
      {query.data?.coverage.errors.map((error, index) => (
        <p role="status" key={index}>
          {error}
        </p>
      ))}
      <p className="ae-note">Saved records. Unsaved workspace edits are not included.</p>
      {query.state === "ready" && (
        <Button className="ae-refresh" onClick={() => query.refresh()}>
          Refresh records
        </Button>
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
