import { useDeliveryPortfolio } from "./data.tsx";
import { ReadState } from "./presentation.tsx";
import { useEffect, useState } from "react";
import { Section, useViewParams } from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import { eligibility, groupFor, groupResult, identity, promptFor } from "./lib/index.ts";
import {
  CoverageNotice,
  EmptyGroup,
  PromptBox,
  Ready,
  RecordLink,
  StoryLink,
  RowFacts,
  WorkflowMap,
} from "./presentation.tsx";
import type { PortfolioProps } from "./presentation.tsx";

export function DeliveryStage({ portfolio, status }: PortfolioProps) {
  const params = useViewParams();
  const group = groupFor(typeof params.stage === "string" ? params.stage : "");
  const [selected, setSelected] = useState<string[]>([]);
  const result = group ? groupResult(group.stage, portfolio) : null;
  const rows = result?.state === "records" ? result.rows : [];
  const eligible = rows
    .filter((row) => eligibility(row).eligible)
    .map((row) => identity(row.record.ref));
  const selection = selected.filter((key) => eligible.includes(key));
  const eligibilityKey = JSON.stringify(eligible);
  useEffect(
    () => setSelected((previous) => previous.filter((key) => eligible.includes(key))),
    [eligibilityKey],
  );
  if (!group)
    return (
      <main className="ae-page">
        <h1>Invalid delivery stage</h1>
        <p>This route does not identify a supported stage.</p>
      </main>
    );
  return (
    <main className="ae-page">
      <Ready status={status} />
      <h1>{group.title}</h1>
      <WorkflowMap portfolio={portfolio} current={group.stage} />
      <CoverageNotice portfolio={portfolio} />
      <div className="ae-with-aside">
        <Section
          title={group.title}
          count={`${result?.state === "records" && !result.complete ? "at least " : ""}${rows.length} ${group.unit}`}
          caption={group.rule}
          status={status}
          what={group.title}
          controls={
            <Button onClick={() => setSelected([...new Set(eligible)])} disabled={!eligible.length}>
              Select all visible eligible {group.unit}
            </Button>
          }
        >
          {rows.length ? (
            rows.map((row, index) => {
              const key = identity(row.record.ref);
              const allowed = eligibility(row);
              return (
                <div className="ae-select-row" key={`${key}-${index}`}>
                  <input
                    type="checkbox"
                    aria-label={`Select ${row.record.title}`}
                    disabled={!allowed.eligible}
                    checked={selection.includes(key)}
                    onChange={(event) =>
                      setSelected(
                        event.target.checked
                          ? [...selection, key]
                          : selection.filter((value) => value !== key),
                      )
                    }
                  />
                  <div>
                    {row.kind === "story" ? (
                      <StoryLink story={row.record} parent={row.parent} />
                    ) : (
                      <RecordLink record={row.record} />
                    )}
                    {row.kind === "story" && (
                      <div className="ae-note">
                        Parent <RecordLink record={row.parent} />
                      </div>
                    )}
                    <RowFacts row={row} />
                    {allowed.reason && <p className="ae-note">{allowed.reason}</p>}
                  </div>
                </div>
              );
            })
          ) : (
            <EmptyGroup stage={group.stage} />
          )}
        </Section>
        <aside>
          <PromptBox prompt={promptFor(group.stage, portfolio, selection)} />
        </aside>
      </div>
    </main>
  );
}

export default function Page() {
  const params = useViewParams();
  const valid = groupFor(typeof params.stage === "string" ? params.stage : "");
  const query = useDeliveryPortfolio({ enabled: !!valid, review: params.stage === "review" });
  if (!valid)
    return (
      <main className="ae-page">
        <h1>Invalid delivery stage</h1>
        <p>This route does not identify a supported stage.</p>
      </main>
    );
  return (
    <>
      <ReadState query={query} />
      {query.data && (
        <DeliveryStage portfolio={query.data.portfolio} status={{ status: "ready" }} />
      )}
    </>
  );
}
