// KB overview (SPEC-0135 US1): what the KB is, where to start, how its topics connect, when its
// sources are dated, and what it holds on each topic, all from the orientation query.
import { useEffect } from "react";
import { Section, SectionBody, sectionStatus, useMarkReady } from "@rhizome/kit";
import {
  drawable,
  kbTitle,
  layout,
  matrix,
  timeline,
  MIN_CONCEPTS,
  MIN_DATED,
} from "./lib/site.ts";
import type { Site } from "./lib/site.ts";
import { cmp } from "./lib/kinds.ts";
import {
  GapLink,
  Incomplete,
  ListLink,
  NotEnough,
  RecordTitle,
  label,
  plural,
  useKbFacts,
  useSite,
} from "./site/data.tsx";
import { KindLegend, TopicMap } from "./site/map.tsx";
import { MarkLegend, Timeline } from "./site/timeline.tsx";
import { Matrix } from "./site/matrix.tsx";
import "./knowledge.css";

function Topics({ site }: { site: Site }) {
  const n = site.concepts.length,
    members = (c: string) => site.members.get(c)!.length;
  if (!drawable(site).map)
    return (
      <Section title="Topics and how they connect" count={n}>
        <NotEnough>
          The topic map needs at least {MIN_CONCEPTS} concepts; this KB has {n}.{" "}
          <ListLink view="ao-kb.concepts">List the concepts</ListLink>.
        </NotEnough>
      </Section>
    );
  const l = layout(site),
    hollow = site.concepts.filter((c) => !members(c.path)).length;
  const inAny = site.assertions.filter((a) => site.conceptsOf.get(a.path)?.length).length,
    rest = site.assertions.length - inAny;
  const list = [...site.concepts].sort(
    (a, b) => members(b.path) - members(a.path) || cmp(label(a), label(b)) || cmp(a.path, b.path),
  );
  return (
    <Section
      title="Topics and how they connect"
      count={n}
      caption={
        <>
          Each circle is a concept, its area set by its member assertions: assertions it links, or
          that link it.{" "}
          {site.edges.length
            ? `Lines are the Related Concepts people recorded (${plural(site.edges.length, "pair")}).`
            : "No Related Concepts are recorded, so the map has no lines."}
          {hollow
            ? ` ${plural(hollow, "hollow circle")} ${hollow === 1 ? "has" : "have"} no members.`
            : ""}{" "}
          Rings show the kind-group mix on concepts large enough to read it; a solid grey circle is
          too small for rings, not a concept of Other kinds.
          {l.unlabelled
            ? ` ${plural(l.unlabelled, "concept")} ${l.unlabelled === 1 ? "is" : "are"} left unlabelled where a name would overlap; hover or focus a circle for its name, or use the list beside the map.`
            : ""}
        </>
      }
    >
      <KindLegend site={site} />
      <div className="ak-overmap">
        <TopicMap site={site} label={`Topic map, ${plural(n, "concept")}`} />
        <div>
          <p className="ak-coverage-line">
            <b>{inAny.toLocaleString("en-US")}</b> of {plural(site.assertions.length, "assertion")}{" "}
            belong to at least one concept.
            {rest > 0 && (
              <>
                {" "}
                <GapLink id="assertion-no-concept">
                  {rest.toLocaleString("en-US")} belong to none
                </GapLink>
                .
              </>
            )}
          </p>
          <div
            className="ak-concept-list"
            role="table"
            aria-label="Concepts with member and Related Concepts counts"
          >
            <div role="row" className="ak-concept-row ak-concept-head">
              <span role="columnheader">Concept</span>
              <span role="columnheader">Members · Related</span>
            </div>
            {list.map((c) => (
              <div role="row" key={c.path} className="ak-concept-row">
                <span role="cell">
                  <RecordTitle site={site} path={c.path} />
                </span>
                <span role="cell" className="ak-num">
                  {members(c.path)} · {site.related.get(c.path)?.length ?? 0}
                </span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </Section>
  );
}

function SourcesByDate({ site }: { site: Site }) {
  const { counts } = timeline(site);
  const summary = (
    <>
      {plural(counts.recorded, "source")} with a recorded date,{" "}
      {counts.file ? (
        <GapLink id="source-file-name-date">{counts.file} dated by a date in the file name</GapLink>
      ) : (
        "none dated by a date in the file name"
      )}
      ,{" "}
      {counts.none ? (
        <GapLink id="source-undated">{counts.none} with no date</GapLink>
      ) : (
        "none with no date"
      )}
      {counts.unreadable ? (
        <>
          , and{" "}
          <GapLink id="source-unreadable-date">
            {counts.unreadable} with an unreadable recorded date
          </GapLink>
        </>
      ) : null}
      .
    </>
  );
  if (!drawable(site).timeline)
    return (
      <Section title="Sources by date" count={site.dated.length}>
        <NotEnough>
          Sources by date needs at least {MIN_DATED} dated sources. {summary}
        </NotEnough>
      </Section>
    );
  return (
    <Section
      title="Sources by date"
      count={site.dated.length}
      caption={
        <>
          One row per source folder. {summary}{" "}
          {counts.recorded === 0
            ? "No source in this KB records a date, so every mark comes from a file name. "
            : ""}
          Dates from titles are not used, and long stretches with no sources are drawn as axis
          breaks, so no dated source is trimmed.
        </>
      }
    >
      <MarkLegend />
      <Timeline site={site} />
    </Section>
  );
}

function Holdings({ site }: { site: Site }) {
  if (!drawable(site).matrix)
    return (
      <Section title="What the KB holds on each topic" count={0}>
        <NotEnough>
          No concept has member assertions.{" "}
          <GapLink id="assertion-no-concept">See the assertions in no concept</GapLink>.
        </NotEnough>
      </Section>
    );
  const m = matrix(site);
  return (
    <Section
      title="What the KB holds on each topic"
      count={m.rows.length}
      caption="Rows are concepts with members; columns are the raw kinds this KB records, under their kind groups, then stored confidence. The number under each column counts every assertion once; rows can share assertions, because an assertion can belong to several concepts. Activate a cell to list exactly those assertions."
    >
      <Matrix site={site} />
      <p className="ak-legend">
        Tint grows with the count, from a faint floor at one to the cap at the table's largest cell
        ({m.max}), in the column's kind-group colour.
      </p>
      {m.emptyConcepts.length > 0 && (
        <p className="rz-caption">
          Concepts with no members, not shown as rows:{" "}
          {m.emptyConcepts.map((c, i) => (
            <span key={c}>
              {i > 0 && ", "}
              <RecordTitle site={site} path={c} className="ak-prose-link" />
            </span>
          ))}
          .
        </p>
      )}
    </Section>
  );
}

function OverviewBody({ site, profile, vault }: { site: Site; profile?: string; vault: string }) {
  const title = kbTitle(site.mocs, vault);
  const about = [
    profile ? `Profile: ${profile}` : null,
    plural(site.assertions.length, "assertion"),
    plural(site.concepts.length, "concept"),
    plural(site.sources.length, "source"),
    plural(site.artifacts.length, "artifact"),
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <>
      <header className="ak-site-head">
        <h1>{title.title}</h1>
        <p>{about}</p>
      </header>
      <Incomplete site={site} />
      <section className="ak-start" aria-labelledby="ak-start">
        <h2 id="ak-start" className="rz-section-head">
          Start here
        </h2>
        {site.mocs.length ? (
          <p className="ak-start-links">
            {site.mocs.map((m, i) => (
              <span key={m.path}>
                {i > 0 && <span aria-hidden="true"> · </span>}
                <RecordTitle site={site} path={m.path} className="ak-prose-link" />
              </span>
            ))}
          </p>
        ) : (
          <p className="ak-empty">This KB has no _moc.md and no maps of content under MOCs/.</p>
        )}
      </section>
      <Topics site={site} />
      <SourcesByDate site={site} />
      <Holdings site={site} />
      <p className="ak-footnote">
        Gaps, submissions, findings, and curation documents are on{" "}
        <ListLink view="ao-kb.review">Review</ListLink>.
      </p>
    </>
  );
}

export default function Overview() {
  const { query, site } = useSite(),
    facts = useKbFacts(),
    markReady = useMarkReady();
  useEffect(() => {
    if (site && facts.data) markReady();
  }, [site, facts.data, markReady]);
  return (
    <main className="ak-page ak-site">
      <SectionBody status={sectionStatus(query)} what="the KB overview">
        {() =>
          site && (
            <OverviewBody
              site={site}
              profile={facts.data?.profile}
              vault={facts.data?.vault ?? ""}
            />
          )
        }
      </SectionBody>
    </main>
  );
}
