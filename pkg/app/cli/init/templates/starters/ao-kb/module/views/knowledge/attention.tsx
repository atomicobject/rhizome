import { Fact, Facts } from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import { filterAssertions, observations } from "./lib/knowledge.ts";
import type { Knowledge, Observation } from "./lib/knowledge.ts";
import { modelObservations } from "./lib/model.ts";
import type { ModelSync } from "./lib/model.ts";
import { MODEL_CEILING, useModelBindings, useModelIndex, useSavedAnalysis } from "./data.tsx";
import { savedAssertion } from "./lib/saved-analysis.ts";
import {
  Basis,
  Empty,
  Filters,
  DateValue,
  Fold,
  Page,
  Record,
  today,
  useFilters,
} from "./parts.tsx";

/** One model file's sync observation: the file names its affected rows, never a first row. */
function ModelFile({ group, unknown }: { group: ModelSync; unknown?: boolean }) {
  const file = group.file.notePath.split("/").pop();
  return (
    <li className="ak-observation">
      <div className="ak-observation-head">
        <span className="ak-key">{file}</span>
        <span className="text-muted-foreground text-sm">Model file</span>
      </div>
      <p className="text-sm text-muted-foreground">
        synced <DateValue value={group.file.synced} /> ·{" "}
        {unknown
          ? `comparison unknown for ${group.rows.length} ${group.rows.length === 1 ? "row" : "rows"}`
          : `${group.rows.length} ${group.rows.length === 1 ? "row cites" : "rows cite"} claims updated later`}
      </p>
      <ul className="ak-file-rows">
        {group.rows.map(({ record, changed, unknown: open }) => (
          <li key={record.key}>
            <Record record={{ ref: record.ref, path: record.key, title: record.title }} />{" "}
            {record.aoValue !== record.title && <span className="ak-key">{record.aoValue}</span>}{" "}
            cites{" "}
            {(unknown ? open : changed).map((a, i) => (
              <span key={a.path}>
                {i > 0 && ", "}
                <Record record={a} />,{" "}
                {unknown ? (
                  `updated ${a.updatedAt || "Not recorded"}`
                ) : (
                  <>
                    updated <DateValue value={a.updatedAt} />
                  </>
                )}
              </span>
            ))}
          </li>
        ))}
      </ul>
    </li>
  );
}
function AttentionBody({ data }: { data: Knowledge }) {
  const filters = useFilters(),
    saved = useSavedAnalysis(),
    bindings = useModelBindings(),
    ready = !!bindings.data?.ready,
    index = useModelIndex(ready);
  const model = index.data
    ? modelObservations(index.data.files, index.data.records, index.data.specs, data.assertion)
    : undefined;
  const allowed = new Set(
    filterAssertions(data.assertion, filters.find, filters.concept).map((a) => a.path),
  );
  const groups = observations(data, today());
  for (const d of model?.dangling ?? [])
    groups["Dangling declared references"].push({
      record: { ref: d.record.ref, path: d.record.key, title: d.record.title },
      reason: "Unresolved declared evidence",
      basis: `${d.record.ref.notePath.split("/").pop()} › ${d.record.aoValue} › ${d.field}: ${d.id} names no assertion in the selected product. It may be missing, ambiguous, or in another product.`,
      risk: true,
    } satisfies Observation);
  for (const r of model?.incomplete ?? [])
    groups["Dangling declared references"].push({
      record: { ref: r.ref, path: r.key, title: r.title },
      reason: "Evidence not fully read",
      basis: `${r.ref.notePath.split("/").pop()} › ${r.aoValue} has more evidence relationships than one read returns, so its sync observations may be incomplete. Its declared evidence is still checked value by value.`,
    } satisfies Observation);
  const files = {
    "Knowledge updated since sync": model?.sync ?? [],
    "Sync date unknown": model?.unknown ?? [],
  } as Record<string, ModelSync[]>;
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
            ? `AO saved analysis as of ${saved.data.meta.asOf || "Not recorded"} · ${saved.data.meta.builder || "Builder not recorded"} · .kb/index/kb-weights.json. ${stale === 1 ? "1 claim was updated after that date, so its saved band may be stale" : `${stale} claims were updated after that date, so their saved bands may be stale`}. A claim with no known later edit is not proven fresh.`
            : "AO saved analysis unavailable: .kb/index/kb-weights.json. This is not a clean result."}
      </p>
      {Object.entries(groups).map(([title, observations], groupIndex) => {
        const rows = observations.filter((o) => inScope(o.record) || o.related?.some(inScope));
        const fileGroups = (files[title] ?? []).filter((g) =>
          g.rows.some(
            (r) =>
              inScope({ path: r.record.key, title: r.record.title }) ||
              [...r.changed, ...r.unknown].some(inScope),
          ),
        );
        return (
          <Fold key={title} title={title} count={rows.length + fileGroups.length}>
            {rows.length || fileGroups.length ? (
              <ul className="ak-list">
                {fileGroups.map((group) => (
                  <ModelFile
                    key={group.file.notePath}
                    group={group}
                    unknown={title === "Sync date unknown"}
                  />
                ))}
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
      <Fold
        title="Saved assertion bands and basis"
        defaultOpen={false}
        count={saved.data ? inSnapshot.length : undefined}
      >
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
      {bindings.data && !ready && (
        <p className="ak-footnote">
          Product model bindings are not ready, so model sync dates and declared model evidence are
          not checked.
        </p>
      )}
      {ready && index.isPending && <p className="ak-footnote">Reading product-model records…</p>}
      {index.error && (
        <p role="alert" className="text-risk text-sm">
          Product-model observations could not be read: {index.error.message}{" "}
          <Button size="sm" onClick={() => void index.refetch()}>
            Retry
          </Button>
        </p>
      )}
      {index.data?.capped && (
        <p role="alert" className="text-risk text-sm">
          Incomplete: model observations cover the first {MODEL_CEILING} model records and 200 model
          files.
        </p>
      )}
    </>
  );
}
export default function Attention() {
  return <Page>{({ data }) => <AttentionBody data={data} />}</Page>;
}
