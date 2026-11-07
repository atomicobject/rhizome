import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { embedded, MoreToggle, Section, sectionStatus, useViewContext } from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import { loadSourceDocument } from "./lib/source-document.ts";
import { relationships, sourceText } from "./lib/knowledge.ts";
import type { Knowledge, Source } from "./lib/knowledge.ts";
import { splitLines } from "./lib/locators.ts";
import { DateValue, Field, Page, Record, TraceLink } from "./parts.tsx";
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
  const graph = relationships(data);
  const doc = useQuery({
    queryKey: ["ao-kb", "source-document", s, data],
    enabled: read,
    queryFn: () => loadSourceDocument(s, data),
  });
  const paths = sourceText(s).paths;
  const extraction = (s.extractedAssertionIds ?? []).map((id) => ({
    id,
    a: data.assertion.find((a) => a.path === graph.resolve(id)),
  }));
  const unlocated = doc.data?.citers.filter((c) => c.passage.status !== "resolved") ?? [];
  return (
    <>
      <header>
        <div className="ak-record-head">
          <h1>{s.name || s.headline || s.title}</h1>
          <TraceLink record={s} />
        </div>
        <p className="ak-meta font-mono">
          {s.path} · {s.aoId || "ID not recorded"}
        </p>
        {/* The reader shows the server's reason above the pane; a standalone page shows it here. */}
        {!embedded && s.workspace.capabilities.readOnlyReason && (
          <p className="ak-footnote">{s.workspace.capabilities.readOnlyReason}</p>
        )}
      </header>
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
      <Section
        title="Extraction history"
        count={extraction.length}
        caption="AO's extractedAssertionIds. Extraction is never shown as support."
        empty={extraction.length ? undefined : "None recorded"}
      >
        <ul className="ak-list">
          {extraction.map(({ id, a }) => (
            <li key={id}>
              {a ? (
                <>
                  <Record record={a} />
                  <span className="ak-meta">
                    {graph.edges.some(
                      (e) => e.from === a.path && e.to === s.path && e.rel === "cites",
                    )
                      ? "Also cites this source"
                      : "Does not cite this source"}
                  </span>
                </>
              ) : (
                <span className="text-risk">Unresolved or ambiguous assertion ID: {id}</span>
              )}
            </li>
          ))}
        </ul>
      </Section>
      <AOHandoff />
    </>
  );
}
export default function SourcePage() {
  const context = useViewContext();
  return (
    <Page>
      {({ data }) => {
        const path = context.kind === "node" ? context.ref.notePath : "";
        const source = data.source.find((s) => s.path === path);
        return source ? (
          <SourceRecord key={source.path} source={source} data={data} />
        ) : (
          <p>No record named {path || "this source"} here</p>
        );
      }}
    </Page>
  );
}
