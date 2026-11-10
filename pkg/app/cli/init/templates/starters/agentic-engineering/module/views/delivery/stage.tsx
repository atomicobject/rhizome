import { useDeliveryPortfolio, useDeliverySelectedEfforts } from "./data.tsx";
import { useEffect, useState } from "react";
import { Section, useViewParams } from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import {
  eligibility,
  groupFor,
  groupResult,
  identity,
  inventoryComplete,
  promptFor,
  readComplete,
} from "./lib/index.ts";
import type { Row, RecordSummary } from "./lib/index.ts";
import {
  CoverageNotice,
  EmptyGroup,
  PromptBox,
  Ready,
  ReadState,
  RecordLink,
  StoryLink,
  RowFacts,
  WorkflowMap,
  unit,
  parentSpecSummary,
} from "./presentation.tsx";
import type { PortfolioProps } from "./presentation.tsx";

function selectable(row: Row) {
  if (row.kind !== "effort") return eligibility(row);
  return {
    eligible: !!row.record.locator && readComplete(row.record.coverage),
    reason: !row.record.locator
      ? "Durable canonical locator unavailable"
      : !readComplete(row.record.coverage)
        ? "Record read incomplete"
        : null,
  };
}

export function DeliveryStage({
  portfolio,
  status,
  stale = false,
}: PortfolioProps & { stale?: boolean }) {
  const params = useViewParams();
  const group = groupFor(typeof params.stage === "string" ? params.stage : "");
  const [selectedRecords, setSelectedRecords] = useState<RecordSummary[]>([]);
  const selected = selectedRecords.map((record) => identity(record.ref));
  useEffect(() => setSelectedRecords([]), [group?.stage]);
  const baseline = group ? groupResult(group.stage, portfolio) : null;
  const baselineRows = baseline?.state === "records" ? baseline.rows : [];
  const effortRefs = baselineRows
    .filter((row) => row.kind === "effort" && selected.includes(identity(row.record.ref)))
    .map((row) => row.record.ref);
  const details = useDeliverySelectedEfforts(effortRefs, portfolio.effortCoverage.graphGeneration);
  const coherent =
    details.data && inventoryComplete([portfolio.effortCoverage, details.data.coverage]);
  const loaded = new Map(
    details.data && coherent
      ? details.data.efforts.map((record) => [identity(record.ref), record])
      : [],
  );
  const current = {
    ...portfolio,
    efforts: portfolio.efforts.map((record) => loaded.get(identity(record.ref)) ?? record),
  };
  const result = group ? groupResult(group.stage, current) : null;
  const rows = result?.state === "records" ? result.rows : [];
  const eligible = rows
    .filter((row) => selectable(row).eligible)
    .map((row) => identity(row.record.ref));
  const unavailable = selectedRecords.filter((record) => !eligible.includes(identity(record.ref)));
  if (!group)
    return (
      <main className="ae-page">
        <h1>Invalid delivery stage</h1>
        <p>This route does not identify a supported stage.</p>
      </main>
    );
  const composed = promptFor(group.stage, current, selected);
  const loadingScope = effortRefs.length > 0 && (details.isPending || details.isFetching);
  const incompleteScope = effortRefs.length > 0 && details.data && !coherent;
  const readReasons = [
    ...(stale ? ["Refresh must finish successfully before copying."] : []),
    ...(loadingScope ? ["Reading selected effort scope."] : []),
    ...(details.error ? ["Selected effort read failed."] : []),
    ...(incompleteScope
      ? ["Selected effort evidence is incomplete or spans different graph generations."]
      : []),
  ];
  const prompt = readReasons.length
    ? {
        text: [...readReasons, composed.text || composed.reasons.join(". ")].join("\n\n"),
        reasons: [...readReasons, ...composed.reasons],
        copyable: false,
      }
    : composed;
  function toggle(keys: string[], checked: boolean) {
    setSelectedRecords((previous) =>
      checked
        ? [
            ...previous,
            ...rows
              .filter(
                (row) =>
                  keys.includes(identity(row.record.ref)) &&
                  !previous.some((record) => identity(record.ref) === identity(row.record.ref)),
              )
              .map((row) => row.record),
          ]
        : previous.filter((record) => !keys.includes(identity(record.ref))),
    );
  }
  function renderRow(row: Row, index: number) {
    const key = identity(row.record.ref);
    const allowed = selectable(row);
    return (
      <div className="ae-select-row" key={`${key}-${index}`}>
        <input
          type="checkbox"
          aria-label={`Select ${row.record.title}`}
          disabled={!allowed.eligible}
          checked={selected.includes(key)}
          onChange={(event) => toggle([key], event.target.checked)}
        />
        <div>
          {row.kind === "story" ? (
            <StoryLink story={row.record} parent={row.parent} />
          ) : (
            <RecordLink record={row.record} />
          )}
          <RowFacts row={row} />
          {allowed.reason && <p className="ae-note">{allowed.reason}</p>}
        </div>
      </div>
    );
  }
  const parents = new Map<string, Extract<Row, { kind: "story" }>[]>();
  for (const row of rows)
    if (row.kind === "story") {
      const key = identity(row.parent.ref);
      const siblings = parents.get(key) ?? [];
      siblings.push(row);
      parents.set(key, siblings);
    }
  return (
    <main className="ae-page">
      <Ready status={status} />
      <h1>{group.title}</h1>
      <WorkflowMap portfolio={current} current={group.stage} />
      <CoverageNotice portfolio={current} />
      <div className="ae-with-aside">
        <Section
          title={group.title}
          count={`${result?.state === "records" && !result.complete ? "at least " : ""}${rows.length} ${unit(rows.length, group.unit)}${result ? parentSpecSummary(result) : ""}`}
          caption={group.rule}
          status={status}
          what={group.title}
          controls={
            <Button
              onClick={() =>
                setSelectedRecords(
                  rows
                    .filter((row) => eligible.includes(identity(row.record.ref)))
                    .map((row) => row.record),
                )
              }
              disabled={!eligible.length || unavailable.length > 0}
            >
              Select all visible eligible {group.unit}
            </Button>
          }
        >
          {group.stage === "ready"
            ? [...parents.values()].map((siblings) => {
                const parent = siblings[0].parent;
                const keys = siblings
                  .filter((row) => selectable(row).eligible)
                  .map((row) => identity(row.record.ref));
                const checked = keys.length > 0 && keys.every((key) => selected.includes(key));
                const mixed = !checked && keys.some((key) => selected.includes(key));
                return (
                  <section className="ae-story-group" key={identity(parent.ref)}>
                    <div className="ae-select-row">
                      <input
                        type="checkbox"
                        aria-label={`Select ${keys.length} visible eligible ready stories in ${parent.title}`}
                        ref={(element) => {
                          if (element) element.indeterminate = mixed;
                        }}
                        checked={checked}
                        disabled={!keys.length}
                        onChange={(event) => toggle(keys, event.target.checked)}
                      />
                      <div>
                        <RecordLink record={parent} />
                        <p className="ae-note">
                          Recorded {parent.status}. {keys.length} visible eligible ready stories.
                        </p>
                      </div>
                    </div>
                    {siblings.map(renderRow)}
                  </section>
                );
              })
            : rows.map(renderRow)}
          {!rows.length && <EmptyGroup stage={group.stage} />}
        </Section>
        <aside>
          {unavailable.length > 0 && (
            <div role="alert">
              <p>
                {unavailable.length} selected{" "}
                {unavailable.length === 1 ? "record is" : "records are"} no longer eligible or
                visible. Review this change before copying.
              </p>
              <ul>
                {unavailable.map((record) => (
                  <li key={identity(record.ref)}>
                    Previously selected: {record.title} · {record.locator}
                  </li>
                ))}
              </ul>
              <Button
                onClick={() =>
                  setSelectedRecords((previous) =>
                    previous.filter((record) => eligible.includes(identity(record.ref))),
                  )
                }
              >
                Use remaining eligible selection
              </Button>
            </div>
          )}
          {selected.length > 0 && (
            <Button onClick={() => setSelectedRecords([])}>Clear selection</Button>
          )}
          {loadingScope && <p role="status">Reading selected effort scope…</p>}
          {details.error && (
            <p role="alert">
              Selected effort read failed. {details.error.message}{" "}
              <Button onClick={() => details.refetch()}>Read selection again</Button>
            </p>
          )}
          {!loadingScope && incompleteScope && (
            <p role="status">
              Some selected scope is incomplete. Inspect the source before copying.
            </p>
          )}
          <PromptBox prompt={prompt} />
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
        <DeliveryStage
          portfolio={query.data.portfolio}
          status={{ status: "ready" }}
          stale={query.retained || query.isFetching}
        />
      )}
    </>
  );
}
