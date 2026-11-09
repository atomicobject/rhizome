// The source page's site parts (SPEC-0135 US2, US3): the "Open in source" target named by the
// `assertion` param, the source note with the paragraph that links the assertion, where the
// source sits, and the assertions extracted from it kept apart from those that cite it.
import { useEffect, useMemo } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { Section, SectionBody, sectionStatus, useGraphQL, useMarkReady } from "@rhizome/kit";
import { relationships } from "./lib/knowledge.ts";
import type { Knowledge } from "./lib/knowledge.ts";
import { noteBody } from "./lib/locators.ts";
import { sourceCandidates, sourceScope } from "./lib/site.ts";
import type { CitationRead, Link, Site } from "./lib/site.ts";
import { sourceScopeQuery } from "./lib/queries.ts";
import { inlineState, useCitationStates } from "./citations.tsx";
import type { CitationStateRow } from "./citations.tsx";
import { FileLink, Lines } from "./evidence.tsx";
import { AssertionItem, Body, RecordTitle, plural, readCitations } from "./site/data.tsx";
import { ScopedPair } from "./site/scoped.tsx";

export const LINKED_HERE = "This source links to the assertion here";

type Target =
  | { assertion: string; missing: true }
  | {
      assertion: string;
      a: Knowledge["assertion"][number];
      row?: CitationStateRow;
      reading: boolean;
    };

/** The citation of `assertion` that names this source, with its US3 state, for `children`. */
export function WithTarget({
  source,
  assertion,
  data,
  children,
}: {
  source: string;
  assertion: string;
  data: Knowledge;
  children: (target: Target) => ReactNode;
}) {
  const a = data.assertion.find((x) => x.path === assertion);
  if (!a) return <>{children({ assertion, missing: true })}</>;
  return (
    <Cited source={source} a={a} data={data}>
      {children}
    </Cited>
  );
}
function Cited({
  source,
  a,
  data,
  children,
}: {
  source: string;
  a: Knowledge["assertion"][number];
  data: Knowledge;
  children: (target: Target) => ReactNode;
}) {
  const graph = useMemo(() => relationships(data), [data]);
  const states = useCitationStates(a, data, graph);
  return (
    <>
      {children({
        assertion: a.path,
        a,
        row: states.rows?.find((r) => r.citation.source?.path === source),
        reading: !states.rows,
      })}
    </>
  );
}

/** The banner "Open in source" lands on: what the KB records about this citation. */
export function TargetBanner({ target }: { target: Target }) {
  if ("missing" in target)
    return (
      <p className="ak-target" role="status">
        Opened for {target.assertion}, which is not an indexed assertion.
      </p>
    );
  const { a, row, reading } = target;
  if (reading)
    return (
      <p className="ak-target rz-loading" role="status">
        Reading the citation…
      </p>
    );
  const name = a.headline || a.title;
  if (!row)
    return (
      <p className="ak-target" role="status">
        {name} does not cite this source.
      </p>
    );
  return (
    <div className="ak-target" role="status">
      <p>
        Opened from <b>{name}</b>: {inlineState(row)}.{" "}
        {row.state === "linked" && `${LINKED_HERE}, highlighted below.`}
        {row.state === "not-linked" && "This source does not link to the assertion."}
        {row.state === "locator" && "Nothing is highlighted."}
      </p>
      {row.state === "passage" && <Passage row={row} />}
    </div>
  );
}

function Passage({ row }: { row: CitationStateRow }) {
  const p = row.passage;
  if (p.status !== "resolved") return null;
  return (
    <>
      <Lines passage={p} />
      <FileLink path={p.path} line={p.startLine}>
        Open the recorded passage at line {p.startLine}
      </FileLink>
    </>
  );
}

/** The source note's own body, with the paragraph that links the target assertion highlighted. */
export function SourceNote({
  content,
  site,
  target,
}: {
  content: string;
  site?: Site;
  target?: Target;
}) {
  const row = target && "row" in target ? target.row : undefined;
  const mark = row?.state === "linked" ? row.paragraph : -1;
  useEffect(() => {
    if (mark >= 0) document.getElementById("ak-body-mark")?.scrollIntoView({ block: "center" });
  }, [mark]);
  return (
    <Section title="Source note">
      <Body text={noteBody(content)} site={site} mark={mark} markLabel={LINKED_HERE} />
    </Section>
  );
}

/** Where the source sits, what was extracted from it, and which assertions cite it. */
export function SourceScope({ site, path }: { site: Site; path: string }) {
  const read = useGraphQL<{ node: { linkingAssertions: (Link & CitationRead)[] } | null }>(
    sourceScopeQuery(path),
  );
  const linking = read.data?.node?.linkingAssertions;
  const extra = linking
    ? sourceCandidates(site, path, linking).filter((p) => !linking.some((l) => l.path === p))
    : [];
  const provenance = useQuery({
    queryKey: ["ao-kb", "source-provenance", path, extra],
    enabled: !!linking,
    queryFn: ({ signal }) => readCitations(extra, signal),
  });
  const markReady = useMarkReady();
  useEffect(() => {
    if (provenance.data) markReady();
  }, [provenance.data, markReady]);
  const extraction = site.extraction.get(path) ?? { resolved: [], unresolved: [] };
  return (
    <SectionBody status={sectionStatus(read, provenance)} what="the assertions that cite it">
      {() => {
        const scope = sourceScope(site, path, [
          ...(linking ?? []),
          ...(provenance.data?.nodes ?? []),
        ]);
        const cites = new Set(scope.citing);
        return (
          <>
            <ScopedPair
              site={site}
              scope={scope}
              ring={path}
              mapCaption="In scope: the concepts of the assertions that cite this source. Ringed on the time axis: this source."
            />
            <Section
              title="Assertions that cite it"
              count={scope.citing.length}
              caption="Assertions whose Sources section links this source, or whose provenance.source names it."
              empty={scope.citing.length ? undefined : "None recorded."}
            >
              <ul className="ak-list">
                {scope.citing.map((a) => (
                  <AssertionItem key={a} site={site} path={a} />
                ))}
              </ul>
            </Section>
            <Section
              title="Assertions extracted from it"
              count={extraction.resolved.length + extraction.unresolved.length}
              caption="AO's extractedAssertionIds. Extraction is never shown as support."
              empty={
                extraction.resolved.length + extraction.unresolved.length
                  ? undefined
                  : "None recorded."
              }
            >
              <ul className="ak-list">
                {extraction.resolved.map((a) => (
                  <li key={a}>
                    <RecordTitle site={site} path={a} />
                    <span className="ak-meta">
                      {cites.has(a) ? "Also cites this source" : "Does not cite this source"}
                    </span>
                  </li>
                ))}
                {extraction.unresolved.map((id, i) => (
                  <li key={`${id}-${i}`}>
                    <code className="ak-path">{id}</code>
                    <span className="ak-meta text-risk">
                      Resolves to no assertion, or to several
                    </span>
                  </li>
                ))}
              </ul>
              {extraction.unresolved.length > 0 && (
                <p className="ak-footnote">
                  {plural(extraction.unresolved.length, "extraction ID")} shown as written.
                </p>
              )}
            </Section>
          </>
        );
      }}
    </SectionBody>
  );
}
