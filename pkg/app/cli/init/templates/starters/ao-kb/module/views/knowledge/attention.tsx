import { Fact, Facts } from "@rhizome/kit";
import { filterAssertions, observations } from "./lib/knowledge.ts";
import type { Knowledge } from "./lib/knowledge.ts";
import { useSavedAnalysis } from "./data.tsx";
import { savedAssertion } from "./lib/saved-analysis.ts";
import {
  Basis,
  Empty,
  Filters,
  Fold,
  ModelUnavailable,
  Page,
  Record,
  today,
  useFilters,
} from "./parts.tsx";

function AttentionBody({ data }: { data: Knowledge }) {
  const filters = useFilters(),
    saved = useSavedAnalysis();
  const allowed = new Set(
    filterAssertions(data.assertion, filters.find, filters.concept).map((a) => a.path),
  );
  const groups = observations(data, today());
  const inScope = (r: {
    path: string;
    title: string;
    headline?: string | null;
    name?: string | null;
  }) =>
    allowed.has(r.path) ||
    (!r.path.startsWith("assertions/") &&
      !filters.concept &&
      (!filters.find ||
        `${r.headline || r.name || r.title}`.toLowerCase().includes(filters.find.toLowerCase())));
  const assessments = data.assertion
    .filter((a) => allowed.has(a.path))
    .map((a) => ({
      a,
      assessment: savedAssertion(saved.data, a.aoId || "", a.updatedAt || undefined),
    }));
  const inSnapshot = assessments.filter((s) => s.assessment.status === "available");
  const missing = assessments.length - inSnapshot.length;
  const stale = inSnapshot.filter(
    (s) => s.assessment.status === "available" && s.assessment.mayBeStale,
  ).length;
  return (
    <>
      <Facts note="Structural observations from authored links and recorded dates. AO status remains a separate stored fact.">
        <Fact value={data.assertion.length}>
          {data.assertion.length === 1 ? "assertion" : "assertions"}
        </Fact>
        <Fact value={data.source.length}>{data.source.length === 1 ? "source" : "sources"}</Fact>
      </Facts>
      <Filters data={data} />
      <p className="ak-basis">
        {saved.isPending
          ? "Reading AO saved analysis…"
          : saved.data
            ? `AO saved analysis as of ${saved.data.meta.asOf || "Not recorded"} · ${saved.data.meta.builder || "Builder not recorded"} · .kb/index/kb-weights.json. ${stale} ${stale === 1 ? "claim" : "claims"} updated after that date; their saved bands may be stale. No known later edit does not prove freshness.`
            : "AO saved analysis unavailable: .kb/index/kb-weights.json. This is not a clean result."}
      </p>
      {Object.entries(groups).map(([title, observations], groupIndex) => {
        const rows = observations.filter((o) => inScope(o.record) || o.related?.some(inScope));
        return (
          <Fold key={title} title={title} count={rows.length}>
            {rows.length ? (
              <ul className="ak-list">
                {rows.map((o, i) => (
                  <li key={`${o.record.path}-${i}`} className="ak-observation">
                    <div className="ak-observation-head">
                      <Record record={o.record} />
                      {o.related?.map((r) => (
                        <span key={r.path}>
                          {" "}
                          · <Record record={r} />
                        </span>
                      ))}
                      <span
                        className={o.risk ? "text-risk text-sm" : "text-muted-foreground text-sm"}
                      >
                        {o.reason}
                      </span>
                      <Basis id={`basis-${groupIndex}-${i}`}>{o.basis}</Basis>
                    </div>
                  </li>
                ))}
              </ul>
            ) : (
              <Empty />
            )}
          </Fold>
        );
      })}
      <p className="ak-saved-divider">From AO's saved analysis</p>
      {saved.data && missing > 0 && (
        <p className="ak-footnote">
          {missing} {missing === 1 ? "claim is" : "claims are"} not in the snapshot.
        </p>
      )}
      <Fold title="Saved assertion bands and basis" defaultOpen={false} count={saved.data ? inSnapshot.length : undefined}>
        <p className="ak-footnote">
          As of {saved.data?.meta.asOf || "Not recorded"} · built{" "}
          {saved.data?.meta.generatedAt || "Not recorded"} ·{" "}
          {saved.data?.meta.builder || "Builder not recorded"} · .kb/index/kb-weights.json
        </p>
        {!saved.data ? (
          <p>Unavailable. This is not a clean result.</p>
        ) : inSnapshot.length ? (
          <ul className="ak-list">
            {inSnapshot.map(({ a, assessment }) => (
              <li key={a.path}>
                <Record record={a} />
                {assessment.status === "available" ? (
                  <>
                    <span className="ak-status">{assessment.band}</span>
                    {assessment.mayBeStale && (
                      <span className="ak-meta">
                        May be stale: claim updated after the as-of date
                      </span>
                    )}
                    <p className="text-sm whitespace-pre-wrap">{assessment.basis}</p>
                  </>
                ) : (
                  <p>Not in snapshot</p>
                )}
              </li>
            ))}
          </ul>
        ) : (
          <Empty />
        )}
      </Fold>
      <ModelUnavailable />
    </>
  );
}
export default function Attention() {
  return <Page>{({ data }) => <AttentionBody data={data} />}</Page>;
}
