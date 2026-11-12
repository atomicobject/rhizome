import { useDeliveryPortfolio } from "./data.tsx";
import { ReadState } from "./presentation.tsx";
import { useState } from "react";
import { Section } from "@rhizome/kit";
import { groupResult, firstDayPrompt, identity } from "./lib/index.ts";
import {
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
      title={group.title}
      count={`${result.complete ? "" : "at least "}${result.count} ${unit(result.count, group.unit)}${parentSpecSummary(result)}`}
    >
      {result.rows.length ? (
        <>
          {result.rows.slice(0, 4).map((row, index) => (
            <div className="ae-row" key={`${identity(row.record.ref)}-${index}`}>
              <div className="ae-row-main">
                {row.kind === "story" ? (
                  <StoryLink story={row.record} parent={row.parent} />
                ) : (
                  <RecordLink record={row.record} />
                )}
                <RowFacts row={row} hideStatus={stage !== "shaping" && stage !== "history"} />
              </div>
            </div>
          ))}
          <p className="ae-more">
            <StageLink stage={stage}>
              {result.count > 4
                ? `View all ${result.complete ? "" : "≥"}${result.count} →`
                : `Open ${group.title.toLowerCase()} →`}
            </StageLink>
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
      <h1 className="ae-visually-hidden">Delivery</h1>
      {
        <>
          <WorkflowMap portfolio={portfolio} />
          <p className="ae-note ae-map-note">Groups can overlap, so counts don't add up.</p>
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
              <GroupPreview stage="shaping" portfolio={portfolio} />
              <GroupPreview stage="ready" portfolio={portfolio} />
            </div>
            <div>
              <GroupPreview stage="planned" portfolio={portfolio} />
              <GroupPreview stage="active" portfolio={portfolio} />
              <GroupPreview stage="history" portfolio={portfolio} />
            </div>
          </div>
          <nav className="ae-support" aria-label="Supporting delivery views">
            <StageLink stage="contracts">Current contracts →</StageLink>
            <StageLink stage="review">Record review →</StageLink>
          </nav>
        </>
      }
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
