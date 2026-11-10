// The scoped pair (SPEC-0135 US2, R4): where a concept, source, or artifact sits on the KB
// overview's map and time axis, each half with its own "not enough to draw yet" state.
import { Section } from "@rhizome/kit";
import { scopedPair } from "../lib/site.ts";
import type { Scope, Site } from "../lib/site.ts";
import { MIN_CONCEPTS, MIN_DATED } from "../lib/site.ts";
import { cmp } from "../lib/kinds.ts";
import { TopicMap } from "./map.tsx";
import { MarkLegend, Strip } from "./timeline.tsx";
import { GapLink, ListLink, NotEnough, RecordTitle, label, plural } from "./data.tsx";

function Links({ site, paths }: { site: Site; paths: string[] }) {
  return (
    <>
      {paths.map((p, i) => (
        <span key={p}>
          {i > 0 && ", "}
          <RecordTitle site={site} path={p} className="ak-prose-link" />
        </span>
      ))}
    </>
  );
}

/** The drawn crop's caption after `mapCaption`: only what the crop draws (R4 highlight rule). */
function drawnCaption({
  faint,
  edges,
  outside,
}: {
  faint: string[];
  edges: unknown[];
  outside: number;
}) {
  const near = faint.length
    ? faint.length === 1
      ? "The faint circle is another concept near it on the KB overview's map. "
      : `The ${faint.length} faint circles are other concepts near it on the KB overview's map. `
    : "";
  const lines = edges.length
    ? "Lines are the Related Concepts people recorded between the concepts drawn."
    : "No Related Concepts are recorded between the concepts drawn.";
  const rest = outside
    ? ` ${plural(outside, "other concept")} of the KB ${outside === 1 ? "lies" : "lie"} outside this crop.`
    : "";
  return near + lines + rest;
}

/** `mapCaption` says what is in scope on the map; `ring` is the source ringed on the strip. */
export function ScopedPair({
  site,
  scope,
  mapCaption,
  ring,
}: {
  site: Site;
  scope: Scope;
  mapCaption: string;
  ring?: string;
}) {
  const pair = scopedPair(site, scope);
  const name = (p: string) => {
    const r = site.byPath.get(p);
    return r ? label(r) : p;
  };
  const topics = [...scope.topics].sort((a, b) => cmp(name(a), name(b)) || cmp(a, b));
  const own = topics.length ? (
    <>
      This record's scope holds {plural(topics.length, "concept")}:{" "}
      <Links site={site} paths={topics} />.
    </>
  ) : (
    "Its scope holds no concept."
  );
  const undated = pair.strip.undated.length ? (
    <>
      {" "}
      {plural(pair.strip.undated.length, "source")} in its scope{" "}
      {pair.strip.undated.length === 1 ? "has" : "have"} no date and{" "}
      {pair.strip.undated.length === 1 ? "is" : "are"} not on the axis:{" "}
      <Links site={site} paths={pair.strip.undated} />.
    </>
  ) : null;
  return (
    <div className="ak-scoped">
      <Section title="Where it sits on the topic map">
        {pair.map.missing === "kb-map" ? (
          <NotEnough>
            The KB's topic map needs at least {MIN_CONCEPTS} concepts; it has {pair.map.concepts}.{" "}
            {own} <ListLink view="ao-kb.concepts">List the concepts</ListLink>.
          </NotEnough>
        ) : pair.map.missing === "no-concept" ? (
          <NotEnough>
            This record's scope holds no concept, so there is nothing to place on the map.{" "}
            <GapLink id="assertion-no-concept">Assertions in no concept</GapLink> ·{" "}
            <ListLink view="ao-kb.overview">The KB's topic map</ListLink>.
          </NotEnough>
        ) : pair.map.missing === "alone" ? (
          <NotEnough>
            {own} No other concept falls inside its crop of the KB overview's map, so there is
            nothing to place it against.{" "}
            <ListLink view="ao-kb.overview">See it on the KB's topic map</ListLink>.
          </NotEnough>
        ) : (
          pair.map.hood && (
            <>
              <TopicMap
                site={site}
                scope={scope.topics}
                mark={scope.mark}
                label="Where it sits on the topic map"
              />
              <p className="rz-caption">
                {mapCaption} {drawnCaption(pair.map.hood)}
              </p>
            </>
          )
        )}
      </Section>
      <Section title="When its sources are dated">
        {!pair.strip.drawable ? (
          <NotEnough>
            The KB has {plural(pair.strip.datedInKb, "dated source")}; the time axis needs{" "}
            {MIN_DATED}. This record's scope holds {plural(scope.sources.size, "source")},{" "}
            {pair.strip.dated.length} dated.{undated}{" "}
            <ListLink view="ao-kb.sources" params={{ gap: "source-undated" }}>
              Sources with no date
            </ListLink>
            .
          </NotEnough>
        ) : (
          <>
            {pair.strip.dated.length > 0 && (
              <Strip site={site} dated={pair.strip.dated} ring={ring} />
            )}
            <p className="rz-caption">
              Drawn against the {plural(pair.strip.datedInKb, "dated source")} in the KB;{" "}
              {plural(pair.strip.dated.length, "dated source")} in this record's scope{" "}
              {pair.strip.dated.length === 1 ? "is" : "are"} marked and the rest are faint.{undated}
            </p>
            {pair.strip.dated.length > 0 && <MarkLegend />}
          </>
        )}
      </Section>
    </div>
  );
}
