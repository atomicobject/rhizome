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
  Portfolio,
  RecordSummary,
  Spec,
  Story,
  Stage,
  Prompt,
  Row,
} from "./lib/index.ts";
import "./delivery.css";

export type PortfolioProps = { portfolio: Portfolio; status: SectionStatus };

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

export function WorkflowMap({
  portfolio,
  current,
  compact = false,
}: {
  portfolio: Portfolio;
  current?: Stage;
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
                return (
                  <div
                    key={group.stage}
                    className="ae-stage"
                    aria-current={current === group.stage ? "page" : undefined}
                  >
                    <StageLink stage={group.stage}>
                      <span className="ae-number">
                        {result.complete ? "" : "at least "}
                        {result.count}
                      </span>{" "}
                      {group.unit}
                      <br />
                      {group.title}
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
          {row.record.storiesCoverage.complete ? "stories" : "loaded stories"}
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
            {row.record.criteriaCoverage.complete ? "criteria" : "loaded criteria"}
          </Fact>
        </>
      )}
      {(row.kind === "effort" || row.kind === "review") && (
        <Fact>
          {row.record.checklist.state === "recorded" || row.record.checklist.state === "empty"
            ? `${row.record.checklist.checked} of ${row.record.checklist.total} checklist items checked`
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
      {query.state === "loading" && (
        <Section title="Delivery" status={{ status: "loading" }} what="delivery records" />
      )}
      {query.error && (
        <p role="alert">
          Read failed. {query.error.message}{" "}
          <Button onClick={() => query.refresh()}>Retry read</Button>
        </p>
      )}
      {query.retained && (
        <p role="status">Showing retained committed data after a failed refresh.</p>
      )}
      {query.state === "partial" && (
        <p role="status">Partial committed read. Missing evidence remains unavailable.</p>
      )}
      {query.data?.coverage.errors.map((error, index) => (
        <p role="status" key={index}>
          {error}
        </p>
      ))}
      <p className="ae-note">
        Committed records. Pending saved edits are not included in this brief.
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
