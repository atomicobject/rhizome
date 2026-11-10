import { useDeliveryPortfolio } from "./data.tsx";
import { ReadState } from "./presentation.tsx";
import { useState } from "react";
import { Section } from "@rhizome/kit";
import { groups, groupResult, firstDayPrompt, identity } from "./lib/index.ts";
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
} from "./presentation.tsx";
import type { PortfolioProps } from "./presentation.tsx";

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
            <div className="ae-columns">
              {groups.map((group) => {
                const result = groupResult(group.stage, portfolio);
                if (result.state !== "records") return null;
                return (
                  <Section
                    key={group.stage}
                    title={
                      group.primary ? (
                        group.title
                      ) : (
                        <StageLink stage={group.stage}>{group.title}</StageLink>
                      )
                    }
                    count={
                      group.stage === "review"
                        ? "deferred"
                        : `${result.complete ? "" : "at least "}${result.count} ${group.unit}`
                    }
                    caption={group.rule}
                  >
                    {group.stage === "review" ? (
                      <p className="ae-note">
                        Open Record review to load exact selected-scope evidence. Observations are
                        deferred here.
                      </p>
                    ) : result.rows.length ? (
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
                        {result.rows.length > 4 && (
                          <StageLink stage={group.stage}>
                            Show all {result.count} {group.unit}
                          </StageLink>
                        )}
                      </>
                    ) : (
                      <EmptyGroup stage={group.stage} />
                    )}
                  </Section>
                );
              })}
            </div>
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
