import {
  Count,
  Fact,
  Facts,
  NodeLink,
  Section,
  sectionStatus,
  openView,
  workspaceViewHref,
} from "@rhizome/kit";
import { COVERAGE, filterAssertions, name } from "./lib/knowledge.ts";
import type { Knowledge } from "./lib/knowledge.ts";
import { useCoverage, useSavedAnalysis } from "./data.tsx";
import { Empty, Filters, Fold, Page, Record, useFilters } from "./parts.tsx";

const COVERAGE_SERIES = ["link", 2, 3, 5, 6] as const;

function ConceptsBody({ data }: { data: Knowledge }) {
  const filters = useFilters(),
    coverage = useCoverage(data),
    saved = useSavedAnalysis();
  const assertions = filterAssertions(data.assertion, "", filters.concept);
  const visible = data.concept.filter(
    (c) =>
      (!filters.concept || c.path === filters.concept) &&
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
              <table className="ak-table">
                <thead>
                  <tr>
                    <th scope="col">Concept</th>
                    <th scope="col">Assertions</th>
                    {COVERAGE.map((s) => (
                      <th scope="col" key={s}>
                        {s}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {visible.map((c) => {
                    const members = assertions.filter((a) =>
                      a.concepts.some((v) => v.path === c.path),
                    );
                    const counts = COVERAGE.map(
                      (label) => members.filter((a) => coverage.data?.get(a.path) === label).length,
                    );
                    return (
                      <tr key={c.path}>
                        <td>
                          <Record record={c} />
                          {members.length > 0 && (
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
                        <td>
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
                            aria-label={`Browse ${members.length} ${members.length === 1 ? "assertion" : "assertions"} for ${name(c)}`}
                          >
                            <Count>{members.length}</Count>{" "}
                            {members.length === 1 ? "assertion" : "assertions"}
                          </a>
                        </td>
                        {counts.map((n, i) => (
                          <td key={COVERAGE[i]}>
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
