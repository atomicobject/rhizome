import { useDeliveryPortfolio } from "./data.tsx";
import { ReadState } from "./presentation.tsx";
import { useState } from "react";
import { Section } from "@rhizome/kit";
import { groupResult, firstDayPrompt, identity } from "./lib/index.ts";
import {
  CoverageNotice,
  EmptyGroup,
  PromptBox,
  Ready,
  RecordLink,
  StoryLink,
  RowFacts,
  StageLink,
  WorkflowMap,
  unit,
  parentSpecSummary,
} from "./presentation.tsx";
import type { Portfolio, Stage } from "./lib/index.ts";
import type { PortfolioProps } from "./presentation.tsx";

function GroupPreview({ stage, portfolio }: { stage: Stage; portfolio: Portfolio }) {
  const result = groupResult(stage, portfolio);
  if (result.state !== "records") return null;
  const { group } = result;
  return (
    <Section
      title={<StageLink stage={stage}>{group.title}</StageLink>}
      count={`${result.complete ? "" : "at least "}${result.count} ${unit(result.count, group.unit)}${parentSpecSummary(result)}`}
      caption={group.rule}
    >
      {result.rows.length ? (
        <>
          {result.rows.slice(0, 4).map((row, index) => (
            <div className="ae-row" key={`${identity(row.record.ref)}-${index}`}>
              {row.kind === "story" ? (
                <StoryLink story={row.record} parent={row.parent} />
              ) : (
                <RecordLink record={row.record} />
              )}
              <RowFacts row={row} />
            </div>
          ))}
          <p className="ae-note">
            Showing {Math.min(4, result.count)} of {result.complete ? "" : "at least "}
            {result.count} {unit(result.count, group.unit)}.{" "}
            <StageLink stage={stage}>Open group</StageLink>
          </p>
        </>
      ) : (
        <EmptyGroup stage={stage} />
      )}
    </Section>
  );
}

export function DeliveryHome({ portfolio, status }: PortfolioProps) {
  const [outcome, setOutcome] = useState("");
  return (
    <main className="ae-page">
      <Ready status={status} />
      <Section title="Delivery" status={status} what="delivery records">
        {() => (
          <>
            <WorkflowMap portfolio={portfolio} />
            <p className="ae-note">
              Groups use recorded facts and may overlap. Counts are not added together. People own
              intent, authorization, and completion truth.
            </p>
            <CoverageNotice portfolio={portfolio} />
            {portfolio.specCoverage.complete &&
              portfolio.effortCoverage.complete &&
              !portfolio.specs.length &&
              !portfolio.efforts.length && (
                <Section
                  title="Start by shaping one spec"
                  caption="Describe the outcome, who it is for, and the behavior you want. The specify workflow will inspect existing contracts and team policy."
                >
                  <label>
                    Outcome
                    <textarea
                      aria-label="Outcome"
                      value={outcome}
                      onChange={(event) => setOutcome(event.target.value)}
                    />
                  </label>
                  <PromptBox prompt={firstDayPrompt(outcome)} />
                </Section>
              )}
            <div className="ae-columns ae-home-work">
              <div>
                <h2>Shape the next slice</h2>
                <p className="ae-note">For designers and development leads.</p>
                <GroupPreview stage="shaping" portfolio={portfolio} />
                <GroupPreview stage="ready" portfolio={portfolio} />
              </div>
              <div>
                <h2>Build and finish</h2>
                <p className="ae-note">For developers and implementing agents.</p>
                <GroupPreview stage="planned" portfolio={portfolio} />
                <GroupPreview stage="active" portfolio={portfolio} />
              </div>
            </div>
            <GroupPreview stage="history" portfolio={portfolio} />
            <nav className="ae-support" aria-label="Supporting delivery views">
              <p>
                <StageLink stage="contracts">Current contracts</StageLink>
                <br />
                <span className="ae-note">
                  Active normative specs and their recorded story states.
                </span>
              </p>
              <p>
                <StageLink stage="review">Record review</StageLink>
                <br />
                <span className="ae-note">
                  Load exact scope and checklist evidence to inspect recorded discrepancies.
                </span>
              </p>
            </nav>
          </>
        )}
      </Section>
    </main>
  );
}

export default function Page() {
  const query = useDeliveryPortfolio();
  return (
    <>
      <ReadState query={query} />
      {query.data && <DeliveryHome portfolio={query.data.portfolio} status={{ status: "ready" }} />}
    </>
  );
}
