import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  embedded,
  Fact,
  Facts,
  MoreToggle,
  RecordPage,
  Section,
  SectionBody,
  sectionStatus,
  useViewContext,
  useViewParams,
} from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import { loadSourceDocument } from "./lib/source-document.ts";
import { objectKey, sourceText } from "./lib/knowledge.ts";
import type { Knowledge, Source } from "./lib/knowledge.ts";
import { splitLines } from "./lib/locators.ts";
import { DateValue, Field, Record, TraceLink } from "./parts.tsx";
import { useKnowledge } from "./data.tsx";
import { useOwnRecord, useSite } from "./site/data.tsx";
import { SourceNote, SourceScope, TargetBanner, WithTarget } from "./source-site.tsx";
import { FileLink } from "./evidence.tsx";
import { AOHandoff } from "./record-parts.tsx";

type Citer = Awaited<ReturnType<typeof loadSourceDocument>>["citers"][number];
function SourceMargin({ rows }: { rows: Citer[] }) {
  const [open, setOpen] = useState(false);
  if (!rows.length) return null;
  return (
    <aside>
      <p className="ak-meta">
        {rows.length} citing {rows.length === 1 ? "claim" : "claims"}
      </p>
      {(open ? rows : rows.slice(0, 2)).map((c, n) => (
        <div key={n}>
          <Record record={c.a} />
          <span className="ak-meta">
            Recorded {c.citation.locator} ·{" "}
            {c.citation.origin ? "origin" : `Sources #${c.citation.slot}`}
          </span>
        </div>
      ))}
      {rows.length > 2 && (
        <MoreToggle hidden={rows.length - 2} open={open} onToggle={() => setOpen(!open)} />
      )}
    </aside>
  );
}

function SourceRecord({ source: s, data }: { source: Source; data: Knowledge }) {
  const [read, setRead] = useState(false);
  const doc = useQuery({
    queryKey: ["ao-kb", "source-document", s.path, objectKey(data)],
    enabled: read,
    queryFn: () => loadSourceDocument(s, data),
  });
  const paths = sourceText(s).paths;
  const unlocated = doc.data?.citers.filter((c) => c.passage.status !== "resolved") ?? [];
  return (
    <>
      <Section title="Source facts">
        <dl className="ak-fields">
          <Field label="Kind">{s.kind}</Field>
          <Field label="Speaker role">{s.speakerRole}</Field>
          <Field label="Ingested">
            <DateValue value={s.ingestedAt} />
          </Field>
          <Field label="Locator scheme">{s.locatorScheme}</Field>
        </dl>
      </Section>
      <Section
        title="Raw paths"
        count={paths.length}
        caption="Original source path slots, as written. Availability is checked only when you load the source text."
        empty={paths.length ? undefined : "None recorded. The source-note body is the fallback."}
      >
        <ol className="ak-list">
          {paths.map((path, i) => (
            <li key={i}>
              <span className="text-sm">Slot {i + 1} · </span>
              <span className="font-mono text-xs break-all">{path || "(blank slot)"}</span>
              <span className="ak-meta">
                {doc.data?.slots[i]?.status || "Availability not checked"}
              </span>
            </li>
          ))}
        </ol>
      </Section>
      <Section
        title="Source text"
        caption="Recorded spans are marked beside their citing claims. Extraction history is separate from support."
        controls={
          <Button
            size="sm"
            disabled={doc.isFetching}
            onClick={() => (read ? void doc.refetch() : setRead(true))}
          >
            {read ? "Reload source text" : "Load source text"}
          </Button>
        }
        status={read ? sectionStatus(doc) : undefined}
        what="source text"
      >
        {!read ? (
          <p className="ak-footnote">
            Load the raw text on demand to read it and see recorded passage margins.
          </p>
        ) : (
          () => (
            <>
              {!doc.data?.files.length && (
                <p className="text-risk">Source text unavailable. No substitute excerpt.</p>
              )}
              {doc.data?.files.map((file) => (
                <section key={file.path} className="ak-evidence">
                  <h3 className="font-mono text-xs">{file.path}</h3>
                  {file.fallback && (
                    <p className="ak-footnote">
                      Source-note body fallback. Line numbers are relative to its trimmed body.
                    </p>
                  )}
                  <FileLink path={file.path}>Open source document</FileLink>
                  <div className="ak-source-text" aria-label={`Text of ${file.path}`}>
                    {splitLines(file.content).map((line, i) => {
                      const spans =
                        doc.data?.citers.filter(
                          (c) =>
                            c.passage.status === "resolved" &&
                            c.passage.path === file.path &&
                            c.passage.startLine <= i + 1 &&
                            c.passage.endLine >= i + 1,
                        ) ?? [];
                      return (
                        <div key={i} className="ak-source-line">
                          <div className="ak-line" data-recorded={spans.length > 0}>
                            <span className="ak-line-number">{i + 1}</span>
                            <span className="ak-line-text">{line || " "}</span>
                          </div>
                          <SourceMargin
                            rows={spans.filter(
                              (c) =>
                                c.passage.status === "resolved" && c.passage.startLine === i + 1,
                            )}
                          />
                        </div>
                      );
                    })}
                  </div>
                </section>
              ))}
              {["document-only", "unsupported", "unresolved"].map((category) => {
                const rows = unlocated.filter((c) =>
                  category === "unresolved"
                    ? !["document-only", "unsupported"].includes(c.passage.status)
                    : c.passage.status === category,
                );
                return (
                  <Section
                    key={category}
                    title={
                      category === "document-only"
                        ? "Document-only citers"
                        : category === "unsupported"
                          ? "Unsupported inline locators"
                          : "Unresolved citers"
                    }
                    count={rows.length}
                    empty={rows.length ? undefined : "None recorded"}
                  >
                    <ul className="ak-list">
                      {rows.map((c, i) => (
                        <li key={`${c.a.path}-${i}`}>
                          <Record record={c.a} />
                          <span className="ak-meta">
                            {c.passage.status === "document-only"
                              ? "No passage locator recorded"
                              : c.passage.status === "unsupported"
                                ? `${c.passage.scheme} locator; open the source to read it`
                                : c.passage.status === "unresolved"
                                  ? c.passage.reason
                                  : c.passage.status}{" "}
                            ·{" "}
                            {c.citation.origin
                              ? "origin citation"
                              : `Sources link #${c.citation.slot}`}
                          </span>
                        </li>
                      ))}
                    </ul>
                  </Section>
                );
              })}
            </>
          )
        )}
      </Section>
      <AOHandoff />
    </>
  );
}
type Own = Source;
const OWN_FIELDS =
  "... on Source { content aoId name headline kind workspace { capabilities { readOnlyReason } } }";
const BASIS = { recorded: "Recorded date", file: "Date in file name", none: "No recorded date" };

export default function SourcePage() {
  const context = useViewContext();
  const params = useViewParams();
  const path = context.kind === "node" ? context.ref.notePath : "";
  const target = typeof params.assertion === "string" ? params.assertion : "";
  const own = useOwnRecord<Own>(path, OWN_FIELDS);
  const state = useKnowledge();
  const { query, site } = useSite();
  const o = own.data?.node;
  const s = state.data?.source.find((v) => v.path === path);
  const date = site?.dates.get(path);
  return (
    <main className="ak-page">
      <RecordPage
        className="ak-record"
        kind={<span className="ak-chip">Source</span>}
        title={o ? o.name || o.headline || o.title : path.split("/").pop()?.replace(/\.md$/, "")}
        actions={o && <TraceLink record={o} />}
        facts={
          o && (
            <Facts
              note={
                <span className="font-mono">
                  {o.path} · {o.aoId || "ID not recorded"}
                </span>
              }
            >
              {date && (
                <Fact>
                  {date.date ?? "No date"} · {BASIS[date.basis]}
                  {date.unreadable ? "; unreadable recorded date" : ""}
                </Fact>
              )}
              <Fact>Source kind: {o.kind ?? "not recorded"}</Fact>
            </Facts>
          )
        }
        status={sectionStatus(own)}
        what="this source"
        partial={
          state.capped
            ? `Incomplete: not every ${state.incomplete.join(", ")} record was read, so citing claims may be missing.`
            : undefined
        }
      >
        {() =>
          o ? (
            <>
              {!embedded && o.workspace?.capabilities.readOnlyReason && (
                <p className="ak-footnote">{o.workspace.capabilities.readOnlyReason}</p>
              )}
              {target && state.data ? (
                <WithTarget source={path} assertion={target} data={state.data}>
                  {(t) => (
                    <>
                      <TargetBanner target={t} />
                      <SourceNote content={o.content ?? ""} site={site} target={t} />
                    </>
                  )}
                </WithTarget>
              ) : (
                <SourceNote content={o.content ?? ""} site={site} />
              )}
              <SectionBody status={sectionStatus(query)} what="where it sits in the KB">
                {() => site && <SourceScope site={site} path={path} />}
              </SectionBody>
              <SectionBody
                status={sectionStatus(state.query)}
                what="the source's text and citations"
              >
                {() =>
                  s && state.data ? (
                    <SourceRecord key={s.path} source={s} data={state.data} />
                  ) : (
                    <p>No record named {path} in the knowledge read</p>
                  )
                }
              </SectionBody>
            </>
          ) : (
            <p>No record named {path || "this source"} here</p>
          )
        }
      </RecordPage>
    </main>
  );
}
