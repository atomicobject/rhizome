import {
  Count,
  Fact,
  Facts,
  NodeLink,
  Section,
  sectionStatus,
  usePageNavigation,
  workspaceViewHref,
} from "@rhizome/kit";
import { COVERAGE, filterAssertions, name } from "./lib/knowledge.ts";
import type { Knowledge } from "./lib/knowledge.ts";
import { useCoverage, useSavedAnalysis } from "./data.tsx";
import { Empty, Filters, Fold, Page, Record, useFilters, useMembership } from "./parts.tsx";

/** The one gap this list holds (its rule lives with the others in `lib/site.ts`). */
const NO_MEMBERS = "concept-no-members";

const COVERAGE_SERIES = ["link", 2, 3, 5, 6] as const;

function ConceptsBody({ data }: { data: Knowledge }) {
  const filters = useFilters(),
    coverage = useCoverage(data),
    saved = useSavedAnalysis(),
    { members, conceptsOf } = useMembership(data),
    { openView } = usePageNavigation();
  const assertions = filterAssertions(data.assertion, "", filters.concept, conceptsOf);
  const visible = data.concept.filter(
    (c) =>
      (!filters.concept || c.path === filters.concept) &&
      (!filters.gap || (filters.gap === NO_MEMBERS && !members.get(c.path)?.length)) &&
      (!filters.find ||
        `${name(c)} ${c.aoId ?? ""}`
          .toLocaleLowerCase()
          .includes(filters.find.toLocaleLowerCase())),
  );
  const totals = COVERAGE.map((label) => ({
    label,
    count: assertions.filter((a) => coverage.data?.get(a.path) === label).length,
  }));
  return (
    <>
      <Filters data={data} />
      {filters.gap && (
        <p className="ak-filter-note">
          {filters.gap === NO_MEMBERS
            ? "Showing concepts with no member assertions: no assertion links the concept, and the concept links no assertion."
            : `No concept gap is named ${filters.gap}.`}
        </p>
      )}
      <Facts note="Locator coverage of one origin citation per assertion. A claim can belong to several concepts; overall totals count it once.">
        <Fact value={assertions.length}>
          {assertions.length === 1 ? "assertion" : "assertions"} in scope
        </Fact>
        <Fact value={visible.length}>{visible.length === 1 ? "concept" : "concepts"} shown</Fact>
      </Facts>
      <Section
        title="Origin-citation coverage"
        status={sectionStatus(coverage)}
        what="locator coverage"
      >
        {() => (
          <>
            <div className="ak-coverage-legend flex flex-wrap gap-x-5 gap-y-2 text-sm">
              {totals.map(({ label, count }, i) => (
                <span key={label}>
                  <span
                    className="inline-block h-2 w-2 mr-1"
                    style={{ background: `var(--series-${COVERAGE_SERIES[i]})` }}
                    aria-hidden="true"
                  />
                  {label} <Count>{count}</Count>
                </span>
              ))}
            </div>
            <div className="ak-table-wrap">
              <table className="ak-table" role="table">
                <thead role="rowgroup">
                  <tr role="row">
                    <th role="columnheader" scope="col">
                      Concept
                    </th>
                    <th role="columnheader" scope="col">
                      Assertions
                    </th>
                    {COVERAGE.map((s) => (
                      <th role="columnheader" scope="col" key={s}>
                        {s}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody role="rowgroup">
                  {visible.map((c) => {
                    const inConcept = new Set(members.get(c.path)),
                      rows = assertions.filter((a) => inConcept.has(a.path));
                    const counts = COVERAGE.map(
                      (label) => rows.filter((a) => coverage.data?.get(a.path) === label).length,
                    );
                    return (
                      <tr key={c.path} role="row">
                        <td role="cell">
                          <Record record={c} />
                          {rows.length > 0 && (
                            <div
                              className="ak-coverage"
                              role="img"
                              aria-label={COVERAGE.map((s, i) => `${counts[i]} ${s}`).join(", ")}
                            >
                              {counts.map(
                                (n, i) =>
                                  n > 0 && (
                                    <span
                                      key={i}
                                      style={{
                                        flex: n,
                                        background: `var(--series-${COVERAGE_SERIES[i]})`,
                                      }}
                                    />
                                  ),
                              )}
                            </div>
                          )}
                          <span className="ak-meta">
                            {c.relatedConceptsSection?.relatedConcepts.length || 0} explicitly
                            related{" "}
                            {c.relatedConceptsSection?.relatedConcepts.length === 1
                              ? "concept"
                              : "concepts"}
                          </span>
                        </td>
                        <td role="cell">
                          <a
                            className="text-link hover:underline"
                            href={workspaceViewHref(
                              "ao-kb.assertions",
                              { kind: "group", group: "Knowledge" },
                              { concept: c.path },
                            )}
                            onClick={(e) => {
                              if (e.ctrlKey || e.metaKey) return;
                              e.preventDefault();
                              openView(
                                "ao-kb.assertions",
                                { kind: "group", group: "Knowledge" },
                                { concept: c.path },
                              );
                            }}
                            aria-label={`Browse ${rows.length} ${rows.length === 1 ? "assertion" : "assertions"} for ${name(c)}`}
                          >
                            <Count>{rows.length}</Count>{" "}
                            {rows.length === 1 ? "assertion" : "assertions"}
                          </a>
                        </td>
                        {counts.map((n, i) => (
                          <td key={COVERAGE[i]} role="cell" data-label={COVERAGE[i]}>
                            <Count>{n}</Count>
                          </td>
                        ))}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            {!visible.length && <Empty />}
          </>
        )}
      </Section>
      <Fold title="AO saved concept output" defaultOpen={false}>
        <p className="ak-footnote">
          Dated AO output only. This presentation does not calculate rollups.
        </p>
        {saved.data ? (
          <>
            <p className="text-sm">
              As of {saved.data.meta.asOf || "Not recorded"} ·{" "}
              {saved.data.meta.builder || "Builder not recorded"} · .kb/index/kb-weights.json
            </p>
            {visible.map((c) => {
              const entry = c.aoId && saved.data?.concepts[c.aoId];
              return (
                <div key={c.path} className="ak-observation">
                  <NodeLink node={c.ref}>{name(c)}</NodeLink>
                  <pre className="whitespace-pre-wrap text-xs">
                    {entry ? JSON.stringify(entry, null, 2) : "Not in snapshot"}
                  </pre>
                </div>
              );
            })}
          </>
        ) : (
          <p>Unavailable: .kb/index/kb-weights.json. This is not a clean result.</p>
        )}
      </Fold>
    </>
  );
}
export default function Concepts() {
  return <Page>{({ data }) => <ConceptsBody data={data} />}</Page>;
}
