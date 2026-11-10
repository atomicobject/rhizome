import type { LiveNode } from "./data.tsx";
import { useDeliveryPortfolio } from "./data.tsx";
import { ReadState } from "./presentation.tsx";
import { useEffect, useState } from "react";
import { Section, useViewLocation, usePageNavigation, getViewInvocation } from "@rhizome/kit";
import { groupFor, groups, identity } from "./lib/index.ts";
import type { RecordSummary } from "./lib/index.ts";
import {
  CoverageNotice,
  Ready,
  RecordLink,
  StageLink,
  WorkflowMap,
  unit,
  useAnnouncedStage,
} from "./presentation.tsx";
import type { PortfolioProps } from "./presentation.tsx";

function recentStorageKey() {
  const { vaultKey, view } = getViewInvocation();
  return JSON.stringify(["agentic-engineering", "recent", vaultKey, view.experienceKey ?? view.id]);
}

function storedVisits(key: string): string[] {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(key) ?? "[]");
    return Array.isArray(value)
      ? value.filter((item): item is string => typeof item === "string").slice(0, 12)
      : [];
  } catch {
    return [];
  }
}

export function DeliveryPanel({ portfolio, status }: PortfolioProps) {
  const location = useViewLocation();
  const nav = usePageNavigation();
  const [find, setFind] = useState("");
  const [storageKey] = useState(recentStorageKey);
  const [visits, setVisits] = useState(() => storedVisits(storageKey));
  const records = [...portfolio.specs, ...portfolio.efforts];
  const context = location.context;
  const current =
    context.kind === "node"
      ? records.find((record) => identity(record.ref) === identity(context.ref))
      : undefined;
  const currentKey = current ? identity(current.ref) : null;
  const announced = useAnnouncedStage();
  const stage =
    context.kind === "standalone" && context.view === "agentic-engineering.stage"
      ? groupFor(announced ?? "")
      : null;
  useEffect(() => {
    if (current)
      setVisits((previous) =>
        [currentKey!, ...previous.filter((key) => key !== currentKey)].slice(0, 12),
      );
  }, [currentKey]);
  useEffect(() => {
    try {
      sessionStorage.setItem(storageKey, JSON.stringify(visits));
    } catch {
      // Keep visits in this panel when browser storage is unavailable.
    }
  }, [storageKey, visits]);
  const recent = visits.flatMap((key) => records.filter((record) => identity(record.ref) === key));
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
        <p className="ae-panel-here">
          {current?.title ||
            (context.kind === "node" ? context.ref.notePath : (stage?.title ?? "Delivery"))}
        </p>
        <button
          className="ae-link"
          onClick={() =>
            nav.openView("agentic-engineering.home", { kind: "group", group: "Delivery" })
          }
        >
          Delivery home
        </button>
        <CoverageNotice portfolio={portfolio} />
      </Section>
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
        <Section title="Find" count={`${matches.length} loaded ${unit(matches.length, "records")}`}>
          {matches.map((record) => (
            <RecordLink key={identity(record.ref)} record={record} />
          ))}
          {!matches.length && <p className="ae-note">No matches in the loaded inventory.</p>}
        </Section>
      )}
      <WorkflowMap portfolio={portfolio} current={stage?.stage} subject={current?.ref} compact />
      <nav aria-label="Supporting delivery views">
        {groups
          .filter((group) => !group.primary)
          .map((group) => (
            <StageLink key={group.stage} stage={group.stage} current={stage === group}>
              {group.title}
            </StageLink>
          ))}
      </nav>
      <Section title="Recent" caption="Up to 12 visits in this repository and window.">
        <ul aria-label="Recent">
          {recent.map((record) => (
            <li key={identity(record.ref)}>
              <RecordLink record={record} />
            </li>
          ))}
        </ul>
        {!recent.length && <p className="ae-note">Open a spec or effort to start this list.</p>}
        {visits.length > recent.length && (
          <p className="ae-note">
            {visits.length - recent.length} recent {unit(visits.length - recent.length, "visits")}{" "}
            {visits.length - recent.length === 1 ? "is" : "are"} not in the loaded inventory.
          </p>
        )}
      </Section>
      <Section title="Native collections">
        {(
          [
            ["SpecLike", "Specs"],
            ["Effort", "Efforts"],
            ["UserStory", "User stories"],
            ["FeatureArea", "Feature areas"],
          ] as const
        ).map(([type, label]) => (
          <button key={type} className="ae-link" onClick={() => nav.openCollection(type)}>
            {label}
          </button>
        ))}
      </Section>
    </aside>
  );
}

export default function Page() {
  const query = useDeliveryPortfolio();
  return (
    <div className="ae-panel-state">
      <ReadState query={query} />
      {query.data && (
        <DeliveryPanel portfolio={query.data.portfolio} status={{ status: "ready" }} />
      )}
    </div>
  );
}
