import { embedded, Section, sectionStatus, useViewContext } from "@rhizome/kit";
import { POSITIONAL, claimText, dateNumber, isPositional, relationships } from "./lib/knowledge.ts";
import { collapseContradictions } from "./lib/relations.ts";
import { ClaimProse } from "./prose.tsx";
import type { Assertion, Knowledge } from "./lib/knowledge.ts";
import { useBindingFreshness, useModelBindings, useModelCiters } from "./data.tsx";
import { syncCompare } from "./lib/model.ts";
import {
  DateValue,
  Field,
  LinkList,
  ModelFreshness,
  Page,
  Record,
  StatusBadges,
  TraceLink,
} from "./parts.tsx";
import { Evidence } from "./evidence.tsx";
import { AOHandoff, Conflicts, History, SavedAssessment } from "./record-parts.tsx";

function AssertionRecord({ a, data }: { a: Assertion; data: Knowledge }) {
  const graph = relationships(data);
  const claims = new Set(data.assertion.map((v) => v.path));
  const related = collapseContradictions(
    graph.edges.filter(
      (e) =>
        claims.has(e.from) &&
        claims.has(e.to) &&
        (e.from === a.path || e.to === a.path) &&
        e.rel !== "supersedes",
    ),
  );
  return (
    <>
      <header>
        <div className="ak-record-head">
          <h1>
            {a.headline || a.title}
            <StatusBadges a={a} edges={graph.edges} />
          </h1>
          <TraceLink record={a} />
        </div>
        <p className="ak-meta font-mono">
          {a.path} · {a.aoId || "ID not recorded"}
        </p>
        {/* The reader shows the server's reason above the pane; a standalone page shows it here. */}
        {!embedded && a.workspace.capabilities.readOnlyReason && (
          <p className="ak-footnote">{a.workspace.capabilities.readOnlyReason}</p>
        )}
      </header>
      <div className="ak-record-layout">
        <div className="ak-record-column">
          <Section title="Claim">
            <ClaimProse text={claimText(a)} graph={graph} />
          </Section>
          <Conflicts a={a} data={data} />
          <Section title="Recorded facts">
            <dl className="ak-fields">
              <Field label="Kind">{a.kind}</Field>
              <Field label="Stored confidence">
                {a.confidence || "Not recorded"}
                <span className="ak-meta">
                  Starting confidence set at extraction; not a verdict
                </span>
              </Field>
              <Field label="Attestation">
                {a.attestation || (
                  <span>
                    Not recorded
                    <span className="ak-meta">
                      AO default:{" "}
                      {["decision", "fact", "need", "rule"].includes(a.kind || "")
                        ? "direct"
                        : "inferred"}
                    </span>
                  </span>
                )}
              </Field>
              <Field label="AO status">{a.status}</Field>
              <Field label="Speaker">{a.provenanceSpeaker ?? a.speaker}</Field>
              <Field label="Speaker role">{a.provenanceSpeakerRole ?? a.speakerRole}</Field>
              <Field label="Valid from">
                <DateValue value={a.validFrom} />
              </Field>
              <Field label="Valid until">
                <DateValue value={a.validUntil} />
              </Field>
              <Field label="Reviewed">
                {a.reviewedBy || a.reviewedAt ? (
                  <>
                    {a.reviewedBy || "Reviewer not recorded"} · <DateValue value={a.reviewedAt} />
                  </>
                ) : (
                  <>
                    Not recorded<span className="ak-meta">AO default: never reviewed</span>
                  </>
                )}
              </Field>
              <Field label="Priority">{a.priority}</Field>
              <Field label="Horizon">{a.horizon}</Field>
              <Field label="Updated">
                <DateValue value={a.updatedAt} />
              </Field>
              <Field label="Extracted by">{a.provenanceExtractedBy ?? a.extractedBy}</Field>
            </dl>
          </Section>
          <History a={a} data={data} />
          <SavedAssessment a={a} />
          <AOHandoff />
        </div>
        <div className="ak-record-column">
          <Evidence a={a} data={data} />
          <Section
            title="Related claims"
            count={related.length}
            empty={related.length ? undefined : "None recorded"}
          >
            <ul className="ak-list">
              {related.map(({ edge: e, bothEnds }) => {
                const from = graph.all.find((r) => r.path === e.from),
                  to = graph.all.find((r) => r.path === e.to);
                return (
                  <li key={`${e.from}-${e.rel}-${e.to}`}>
                    {from && <Record record={from} />}{" "}
                    <span className="text-muted-foreground">{e.rel}</span>{" "}
                    {to && <Record record={to} />}
                    {bothEnds && <span className="ak-meta">Recorded at both ends</span>}
                  </li>
                );
              })}
            </ul>
          </Section>
          {a.connectionsSection && (
            <p className="ak-footnote">
              Inspect every declaration, including unbound conventions, in{" "}
              <Record
                record={{
                  ref: a.connectionsSection.ref,
                  path: a.path,
                  title: "Authored Connections section",
                }}
              />
              .
            </p>
          )}
          <CitedBy a={a} data={data} graph={graph} />
          <LinkList title="Concepts" records={a.concepts} />
          {a.contradictsRows?.length || a.supportsRows?.length ? (
            <Section title="Recorded demo-feedback references">
              <dl>
                <Field label="Contradicts rows">{a.contradictsRows?.join(", ")}</Field>
                <Field label="Supports rows">{a.supportsRows?.join(", ")}</Field>
              </dl>
              <p className="ak-footnote">
                Authored kind/ID values; these are not claim-to-claim relationships.
              </p>
            </Section>
          ) : null}
        </div>
      </div>
    </>
  );
}
function CitedBy({
  a,
  data,
  graph,
}: {
  a: Assertion;
  data: Knowledge;
  graph: ReturnType<typeof relationships>;
}) {
  const rows = graph.edges.filter(
    (e) => e.to === a.path && ["references", "supported-by-claim", "cites"].includes(e.rel),
  );
  const bindings = useModelBindings();
  const ready = !!bindings.data?.ready;
  const model = useModelCiters(a.path, ready);
  // Stale bindings never read as "None recorded": the body shows their notice instead.
  const freshness = useBindingFreshness();
  const current = freshness.data?.state === "ready";
  const records = model.data?.records ?? [];
  const count = rows.length + records.length;
  return (
    <Section
      title="Cited by"
      count={model.data || !ready ? count : undefined}
      caption="Artifacts and product-model records that name this claim. Extraction is separate, on the source."
      status={ready ? sectionStatus(bindings, model, freshness) : sectionStatus(bindings)}
      what="citing records"
      empty={count || (bindings.data && !ready) || (ready && !current) ? undefined : "None recorded"}
    >
      {() => (
        <>
          <ul className="ak-list">
            {rows.map((e) => {
              const record = graph.all.find((r) => r.path === e.from);
              const artifact = data.artifact.find((r) => r.path === e.from);
              const synced = dateNumber(artifact?.graphSyncedAt);
              return (
                record && (
                  <li key={`${e.from}-${e.rel}`}>
                    {e.rel === "supported-by-claim"
                      ? "Supports"
                      : e.rel === "references"
                        ? "Referenced by"
                        : "Cited by"}{" "}
                    <Record record={record} /> · {record.ref.typeName || "Record"} ·{" "}
                    <span className="font-mono text-xs">{record.aoId || "Not recorded"}</span>
                    {artifact && (
                      <span className="ak-meta">
                        {!artifact.graphSyncedAt ? (
                          "sync date not recorded"
                        ) : !Number.isFinite(synced) ||
                          !Number.isFinite(dateNumber(a.updatedAt)) ? (
                          "sync comparison unknown"
                        ) : dateNumber(a.updatedAt) > synced ? (
                          <>
                            knowledge updated since sync (
                            <DateValue value={artifact.graphSyncedAt} />)
                          </>
                        ) : (
                          <>
                            Knowledge sync recorded <DateValue value={artifact.graphSyncedAt} />
                          </>
                        )}
                      </span>
                    )}
                  </li>
                )
              );
            })}
            {records.map((r) => {
              const file = model.data?.files.find((f) => f.notePath === r.ref.notePath);
              const sync = syncCompare(a.updatedAt, file?.synced);
              return (
                <li key={r.key}>
                  Evidence for <Record record={{ ref: r.ref, path: r.key, title: r.title }} /> ·{" "}
                  {model.data?.specs[r.typeName]?.label ?? r.typeName} ·{" "}
                  <span className="font-mono text-xs">{r.aoValue || "Not recorded"}</span>
                  {isPositional(r.ref) && <> · {POSITIONAL}</>}
                  <span className="ak-meta">
                    {!file?.synced ? (
                      `file sync date not recorded in ${r.ref.notePath.split("/").pop()}`
                    ) : sync === "unknown" ? (
                      "sync comparison unknown"
                    ) : sync === "later" ? (
                      <>
                        knowledge updated since file synced (<DateValue value={file.synced} />)
                      </>
                    ) : (
                      <>
                        File synced <DateValue value={file.synced} />
                      </>
                    )}
                  </span>
                </li>
              );
            })}
          </ul>
          {!ready && (
            <p className="ak-footnote">
              Product model bindings are not ready, so model records that rest on this claim are not
              shown.
            </p>
          )}
          {ready && <ModelFreshness />}
          {model.data?.truncated && (
            <p className="ak-footnote text-risk">
              Incomplete: more model records cite this claim than one read returns.
            </p>
          )}
        </>
      )}
    </Section>
  );
}
export default function AssertionPage() {
  const context = useViewContext();
  return (
    <Page>
      {({ data }) => {
        const path = context.kind === "node" ? context.ref.notePath : "";
        const a = data.assertion.find((v) => v.path === path);
        return a ? (
          <AssertionRecord a={a} data={data} />
        ) : (
          <p>No record named {path || "this assertion"} here</p>
        );
      }}
    </Page>
  );
}
