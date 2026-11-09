import { useEffect, useMemo } from "react";
import {
  embedded,
  Fact,
  Facts,
  RecordPage,
  Section,
  SectionBody,
  sectionStatus,
  useMarkReady,
  useViewContext,
} from "@rhizome/kit";
import { POSITIONAL, claimText, dateNumber, isPositional, relationships } from "./lib/knowledge.ts";
import { collapseContradictions } from "./lib/relations.ts";
import { ClaimProse } from "./prose.tsx";
import type { Assertion, Knowledge } from "./lib/knowledge.ts";
import { useBindingFreshness, useKnowledge, useModelBindings, useModelCiters } from "./data.tsx";
import { syncCompare } from "./lib/model.ts";
import {
  DateValue,
  Field,
  ModelFreshness,
  Record,
  StatusBadges,
  TraceLink,
  useMembership,
} from "./parts.tsx";
import { CitationTable, conceptChips } from "./citations.tsx";
import { KIND_GROUPS, NO_KIND, groupOf } from "./lib/kinds.ts";
import { useOwnRecord, useSite } from "./site/data.tsx";
import type { Site } from "./lib/site.ts";
import { Evidence } from "./evidence.tsx";
import { AOHandoff, Conflicts, History, SavedAssessment } from "./record-parts.tsx";

function AssertionRecord({ a, data, site }: { a: Assertion; data: Knowledge; site?: Site }) {
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
      <CitationTable a={a} data={data} graph={graph} site={site} />
      <div className="ak-record-layout">
        <div className="ak-record-column">
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
      empty={
        count || (bindings.data && !ready) || (ready && !current) ? undefined : "None recorded"
      }
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
type Own = Assertion & { legacyKind?: string | null };
const OWN_FIELDS =
  "... on Assertion { content aoId headline kind legacyKind confidence workspace { capabilities { readOnlyReason } } }";

/** US3's inline facts: raw kind with its kind group, stored confidence, and concept chips. */
function InlineFacts({ own, a, data }: { own: Own; a?: Assertion; data?: Knowledge }) {
  const membership = useMembership(data);
  const group = KIND_GROUPS.find((g) => g.id === groupOf(own.kind))!;
  const chips = a && data && membership ? conceptChips(a, data, membership.conceptsOf) : undefined;
  return (
    <Facts
      note={
        <span className="font-mono">
          {own.path} · {own.aoId || "ID not recorded"}
        </span>
      }
    >
      <Fact>
        <i
          className="ak-swatch"
          style={{ background: `var(--kind-${group.id})` }}
          aria-hidden="true"
        />
        {own.kind ?? NO_KIND} · {group.label}
        {own.legacyKind &&
          own.legacyKind !== own.kind &&
          ` (assertionKind records ${own.legacyKind})`}
      </Fact>
      <Fact>Stored confidence: {own.confidence ?? "not recorded"}</Fact>
      {chips && (
        <Fact>
          {chips.length ? (
            <span className="ak-chips">
              {chips.map((c) => (
                <Record key={c!.path} record={c!} />
              ))}
            </span>
          ) : (
            "In no concept"
          )}
        </Fact>
      )}
    </Facts>
  );
}

export default function AssertionPage() {
  const context = useViewContext();
  const path = context.kind === "node" ? context.ref.notePath : "";
  const own = useOwnRecord<Own>(path, OWN_FIELDS);
  const state = useKnowledge();
  const { site } = useSite();
  const markReady = useMarkReady();
  const o = own.data?.node;
  const a = state.data?.assertion.find((v) => v.path === path);
  const graph = useMemo(() => (state.data ? relationships(state.data) : undefined), [state.data]);
  useEffect(() => {
    if (o && state.data) markReady();
  }, [o, state.data, markReady]);
  return (
    <main className="ak-page">
      <RecordPage
        className="ak-record"
        kind={<span className="ak-chip">Assertion</span>}
        title={
          <>
            {o ? o.headline || o.title : path.split("/").pop()?.replace(/\.md$/, "")}
            {a && graph && <StatusBadges a={a} edges={graph.edges} />}
          </>
        }
        actions={o && <TraceLink record={o} />}
        facts={o && <InlineFacts own={o} a={a} data={state.data} />}
        status={sectionStatus(own)}
        what="this assertion"
        partial={
          state.capped
            ? `Incomplete: not every ${state.incomplete.join(", ")} record was read, so relationships may be missing.`
            : undefined
        }
      >
        {() =>
          o ? (
            <>
              {/* The reader shows the server's reason above the pane; a standalone page shows it here. */}
              {!embedded && o.workspace?.capabilities.readOnlyReason && (
                <p className="ak-footnote">{o.workspace.capabilities.readOnlyReason}</p>
              )}
              <Section title="Claim">
                <ClaimProse text={claimText(o)} graph={graph} />
              </Section>
              <SectionBody status={sectionStatus(state.query)} what="evidence and relationships">
                {() =>
                  a && state.data ? (
                    <AssertionRecord a={a} data={state.data} site={site} />
                  ) : (
                    <p>No record named {path} in the knowledge read</p>
                  )
                }
              </SectionBody>
            </>
          ) : (
            <p>No record named {path || "this assertion"} here</p>
          )
        }
      </RecordPage>
    </main>
  );
}
