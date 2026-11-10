import { NodeLink, Section, sectionStatus } from "@rhizome/kit";
import { useQuery } from "@tanstack/react-query";
import { useCitationPassages } from "./data.tsx";
import { readPassage } from "./lib/passage.ts";
import { objectKey, physicalSourceLine, sourceText } from "./lib/knowledge.ts";
import type { Assertion, Citation, Knowledge } from "./lib/knowledge.ts";
import type { PassageResult } from "./lib/locators.ts";
import { Record } from "./parts.tsx";

export function FileLink({
  path,
  line,
  children,
}: {
  path: string;
  line?: number;
  children: React.ReactNode;
}) {
  const params = new URLSearchParams({ file: path });
  if (line) params.set("line", String(line));
  return (
    <a className="text-link hover:underline" href={`/explorer?${params}`} target="_top">
      {children}
    </a>
  );
}
export function Lines({ passage }: { passage: Extract<PassageResult, { status: "resolved" }> }) {
  const lines = [...passage.before, ...passage.lines, ...passage.after];
  const start = passage.startLine - passage.before.length;
  return (
    <div
      className="ak-passage"
      aria-label={`Recorded lines ${passage.startLine} to ${passage.endLine}`}
    >
      {lines.map((line, i) => (
        <div
          className="ak-line"
          data-recorded={start + i >= passage.startLine && start + i <= passage.endLine}
          key={i}
        >
          <span className="ak-line-number">{start + i}</span>
          <span className="ak-line-text">{line || " "}</span>
        </div>
      ))}
    </div>
  );
}
export function Passage({
  citation: c,
  passage: p,
}: {
  citation: Citation;
  passage: PassageResult;
}) {
  const label =
    p.status === "resolved"
      ? "Recorded passage"
      : p.status === "document-only"
        ? "Document only"
        : p.status === "unsupported"
          ? "Unsupported locator"
          : "Unresolved source/locator";
  return (
    <div className="ak-evidence">
      <h3>
        {c.origin ? "Origin citation" : `Also cited · Sources link #${c.slot}`}
        {c.chosenBy && <span className="ak-meta">{c.chosenBy}</span>}
      </h3>
      {c.source ? (
        <Record record={c.source} />
      ) : (
        <span className="text-risk">{c.raw || "Source not recorded"}</span>
      )}
      <p
        className={
          ["resolved", "document-only", "unsupported"].includes(p.status)
            ? "ak-meta"
            : "text-risk text-sm"
        }
      >
        {label}
        {c.locator ? ` · ${c.locator}` : ""}
      </p>
      {p.status === "resolved" ? (
        <>
          <p className="ak-meta font-mono">
            {p.path} · L{p.startLine}-L{p.endLine}
          </p>
          <Lines passage={p} />
          {p.path === c.source?.path && (
            <p className="ak-meta">Recorded line numbers are relative to the source-note body.</p>
          )}
          <p className="ak-meta">{p.hashStatus}</p>
          <FileLink path={p.path} line={physicalSourceLine(c.source, p.path, p.startLine)}>
            Open source at line {physicalSourceLine(c.source, p.path, p.startLine)}
          </FileLink>
        </>
      ) : p.status === "document-only" ? (
        <p className="text-sm">
          No passage locator recorded.{" "}
          {c.source && (
            <NodeLink node={c.source.ref} view="ao-kb.source">
              Open source document
            </NodeLink>
          )}
        </p>
      ) : p.status === "unsupported" ? (
        <p className="text-sm">
          {p.scheme === "timestamp"
            ? "Timestamp locator; open the source to read it"
            : `${p.scheme}: locator; inline passage display is unsupported.`}{" "}
          {c.source && (
            <NodeLink node={c.source.ref} view="ao-kb.source">
              Open source document
            </NodeLink>
          )}
        </p>
      ) : (
        <p className="text-risk text-sm">
          {p.status === "malformed"
            ? "Malformed locator; no passage excerpt is available."
            : p.status === "unresolved"
              ? `${p.reason === "unknown source" ? "Unresolved or ambiguous source" : `Unresolved locator: ${p.reason}`}. No substitute excerpt.`
              : "Source text is unavailable. No substitute excerpt."}
        </p>
      )}
    </div>
  );
}
function Corroboration({ a, data }: { a: Assertion; data: Knowledge }) {
  const query = useQuery({
    queryKey: ["ao-kb", "corroboration", a.path, objectKey(data)],
    queryFn: async () => {
      const { relationships } = await import("./lib/knowledge.ts");
      return Promise.all(
        relationships(data)
          .edges.filter((e) => e.from === a.path && e.rel === "corroborated-by")
          .map(async (e) => {
            const source = data.source.find((s) => s.path === e.to);
            return {
              citation: { raw: e.to, source, origin: false },
              passage: await readPassage(source && sourceText(source), undefined, e.to),
            };
          }),
      );
    },
  });
  return (
    <Section
      title="Corroborated by"
      caption="Corroboration does not change the claim's recorded speaker or origin."
      status={sectionStatus(query)}
      what="corroborating sources"
      empty={query.data?.length === 0 ? "None recorded" : undefined}
    >
      {() =>
        query.data?.map(({ citation, passage }) => (
          <div key={citation.raw} className="ak-evidence">
            {citation.source && <Record record={citation.source} />}
            <p className="ak-meta">
              {passage.status === "document-only"
                ? "No passage locator recorded"
                : "Source unavailable"}
            </p>
          </div>
        ))
      }
    </Section>
  );
}
export function Evidence({ a, data }: { a: Assertion; data: Knowledge }) {
  const query = useCitationPassages(a, data);
  return (
    <>
      <Section
        title="Evidence"
        status={sectionStatus(query)}
        what="recorded evidence"
        empty={query.data?.length === 0 ? "No source recorded. No Sources link." : undefined}
      >
        {() =>
          query.data?.map(({ citation, passage }, i) => (
            <Passage key={i} citation={citation} passage={passage} />
          ))
        }
      </Section>
      <Corroboration a={a} data={data} />
    </>
  );
}
