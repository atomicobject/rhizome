import type { LiveNode } from "./data.tsx";
import { useDeliveryPortfolio } from "./data.tsx";
import { ReadState } from "./presentation.tsx";
import { useEffect, useState } from "react";
import { Section, useViewLocation, usePageNavigation } from "@rhizome/kit";
import { groups, identity } from "./lib/index.ts";
import type { RecordSummary } from "./lib/index.ts";
import { CoverageNotice, Ready, RecordLink, StageLink, WorkflowMap } from "./presentation.tsx";
import type { PortfolioProps } from "./presentation.tsx";

export function DeliveryPanel({ portfolio, status }: PortfolioProps) {
  const location = useViewLocation();
  const nav = usePageNavigation();
  const [find, setFind] = useState("");
  const [recent, setRecent] = useState<RecordSummary[]>([]);
  const records = [...portfolio.specs, ...portfolio.efforts];
  const context = location.context;
  const current =
    context.kind === "node"
      ? records.find((record) => identity(record.ref) === identity(context.ref))
      : undefined;
  const currentKey = current ? identity(current.ref) : null;
  useEffect(() => {
    if (current)
      setRecent((previous) =>
        [current, ...previous.filter((record) => identity(record.ref) !== currentKey)].slice(0, 12),
      );
  }, [currentKey]);
  function searchable(record: RecordSummary & { source?: LiveNode }) {
    const identifiers =
      record.source?.workspace?.fields
        .filter((field) => field.name === "id")
        .flatMap((field) => field.values ?? []) ?? [];
    return [record.title, record.locator ?? record.ref.notePath, ...identifiers]
      .join(" ")
      .toLocaleLowerCase();
  }
  const matches = find.trim()
    ? records.filter((record) => searchable(record).includes(find.trim().toLocaleLowerCase()))
    : [];
  return (
    <aside className="ae-panel">
      <Ready status={status} />
      <Section title="Where you are" status={status} what="delivery orientation">
        <p>{current?.title || (context.kind === "node" ? context.ref.notePath : "Delivery")}</p>
        <button
          className="ae-link"
          onClick={() =>
            nav.openView("agentic-engineering.home", { kind: "group", group: "Delivery" })
          }
        >
          Delivery home
        </button>
        <WorkflowMap portfolio={portfolio} compact />
        <nav aria-label="Supporting delivery views">
          {groups
            .filter((group) => !group.primary)
            .map((group) => (
              <p key={group.stage}>
                <StageLink stage={group.stage}>{group.title}</StageLink>
              </p>
            ))}
        </nav>
        <CoverageNotice portfolio={portfolio} />
        <label>
          Find a spec or effort
          <input
            type="search"
            value={find}
            onChange={(event) => setFind(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && matches[0]) nav.openNode(matches[0].ref);
            }}
          />
        </label>
        {find.trim() && (
          <Section title="Find" count={`${matches.length} loaded records`}>
            {matches.map((record) => (
              <RecordLink key={identity(record.ref)} record={record} />
            ))}
            {!matches.length && <p className="ae-note">No matches in the loaded inventory.</p>}
          </Section>
        )}
        <Section title="Recent" caption="Records visited while this panel is open.">
          <ul aria-label="Recent">
            {recent.map((record) => (
              <li key={identity(record.ref)}>
                <RecordLink record={record} />
              </li>
            ))}
          </ul>
          {!recent.length && <p className="ae-note">Open a spec or effort to start this list.</p>}
        </Section>
        <Section title="Native collections">
          <p>
            <button className="ae-link" onClick={() => nav.openCollection("SpecLike")}>
              Specs
            </button>
          </p>
          <p>
            <button className="ae-link" onClick={() => nav.openCollection("Effort")}>
              Efforts
            </button>
          </p>
          <p>
            <button className="ae-link" onClick={() => nav.openCollection("UserStory")}>
              User stories
            </button>
          </p>
          <p>
            <button className="ae-link" onClick={() => nav.openCollection("FeatureArea")}>
              Feature areas
            </button>
          </p>
        </Section>
      </Section>
    </aside>
  );
}

export default function Page() {
  const query = useDeliveryPortfolio();
  return (
    <>
      <ReadState query={query} />
      {query.data && (
        <DeliveryPanel portfolio={query.data.portfolio} status={{ status: "ready" }} />
      )}
    </>
  );
}
