import { Section, useViewContext } from "@rhizome/kit";
import { claimText, relationships } from "./lib/knowledge.ts";
import type { Assertion, Knowledge } from "./lib/knowledge.ts";
import {
  DateValue,
  Field,
  LinkList,
  ModelUnavailable,
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
  const related = graph.edges.filter(
    (e) =>
      claims.has(e.from) &&
      claims.has(e.to) &&
      (e.from === a.path || e.to === a.path) &&
      e.rel !== "supersedes",
  );
  const citedBy = graph.all.filter((r) =>
    graph.edges.some(
      (e) =>
        e.from === r.path &&
        e.to === a.path &&
        ["references", "supported-by-claim", "cites"].includes(e.rel),
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
        {a.workspace.capabilities.readOnlyReason && (
          <p className="ak-footnote">{a.workspace.capabilities.readOnlyReason}</p>
        )}
      </header>
      <Section title="Claim">
        <p className="ak-claim">{claimText(a) || "None recorded"}</p>
      </Section>
      <Section title="Recorded facts">
        <dl className="ak-fields">
          <Field label="Kind">{a.kind}</Field>
          <Field label="Stored confidence">
            {a.confidence || "Not recorded"}
            <span className="ak-meta">Starting confidence set at extraction; not a verdict</span>
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
            {a.reviewedBy || "Not recorded"} · <DateValue value={a.reviewedAt} />
          </Field>
          <Field label="Priority">{a.priority}</Field>
          <Field label="Horizon">{a.horizon}</Field>
          <Field label="Updated">
            <DateValue value={a.updatedAt} />
          </Field>
          <Field label="Extracted by">{a.provenanceExtractedBy ?? a.extractedBy}</Field>
        </dl>
      </Section>
      <Evidence a={a} data={data} />
      <Conflicts a={a} data={data} />
      <History a={a} data={data} />
      <Section
        title="Related claims"
        count={related.length}
        empty={related.length ? undefined : "None recorded"}
      >
        <ul className="ak-list">
          {related.map((e) => {
            const from = graph.all.find((r) => r.path === e.from),
              to = graph.all.find((r) => r.path === e.to);
            return (
              <li key={`${e.from}-${e.rel}-${e.to}`}>
                {from && <Record record={from} />}{" "}
                <span className="text-muted-foreground">{e.rel}</span>{" "}
                {to && <Record record={to} />}
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
      <LinkList title="Cited by" records={citedBy} />
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
      <SavedAssessment a={a} />
      <ModelUnavailable />
      <AOHandoff />
    </>
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
