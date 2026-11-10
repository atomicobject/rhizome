// US3 citation states (SPEC-0135): one state per citation, from SPEC-0133's evidence states plus
// whether the cited source's body links the assertion, and "Open in source", which passes the
// assertion as the source view's `assertion` param so the target survives tabs and reloads.
import { Section, sectionStatus, usePageNavigation, workspaceViewHref } from "@rhizome/kit";
import { markdownLinks } from "@rhizome/kit/markdown";
import type { ViewNodeRef } from "@rhizome/kit";
import { coverage, name } from "./lib/knowledge.ts";
import type { Assertion, Knowledge, relationships } from "./lib/knowledge.ts";
import { noteBody } from "./lib/locators.ts";
import { citationState, linkedBlock } from "./lib/site.ts";
import type { CitationState, Site } from "./lib/site.ts";
import { useCitationPassages } from "./data.tsx";
import { Record } from "./parts.tsx";

type Graph = ReturnType<typeof relationships>;

export function useCitationStates(a: Assertion, data: Knowledge, graph: Graph) {
  const passages = useCitationPassages(a, data);
  const rows = passages.data?.map(({ citation, passage }) => {
    // The source offset of the first authored link to the assertion, found without rendering.
    const at =
      citation.source && !citation.locator
        ? linkedBlock(markdownLinks(noteBody(citation.source.content)), a.path, graph.resolve)
        : undefined;
    return {
      citation,
      passage,
      at,
      state: citationState(citation, passage, at !== undefined) as CitationState,
    };
  });
  return { query: passages, rows };
}
export type CitationStateRow = NonNullable<ReturnType<typeof useCitationStates>["rows"]>[number];

/** What a citation shows inline. */
export function inlineState(row: CitationStateRow) {
  switch (row.state) {
    case "passage":
      return "Recorded passage";
    case "locator":
      return coverage(row.citation, row.passage);
    case "unresolved":
      return "The cited source does not resolve";
    default:
      return "No passage locator recorded";
  }
}

const BASIS = { recorded: "Recorded date", file: "Date in file name", none: "No recorded date" };

export function OpenInSource({
  source,
  assertion,
}: {
  source: { ref: unknown };
  assertion: string;
}) {
  const { openView } = usePageNavigation();
  const context = { kind: "node", type: "Source", ref: source.ref as ViewNodeRef } as const;
  const params = { assertion };
  return (
    <a
      className="ak-quiet-link"
      href={workspaceViewHref("ao-kb.source", context, params)}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        openView("ao-kb.source", context, params);
      }}
    >
      Open in source
    </a>
  );
}

/** The assertion page's sources inline: each citation with its source's date and basis, its state, and "Open in source". */
export function CitationTable({
  a,
  data,
  graph,
  site,
}: {
  a: Assertion;
  data: Knowledge;
  graph: Graph;
  site?: Site;
}) {
  const { query, rows } = useCitationStates(a, data, graph);
  return (
    <Section
      title="Sources"
      count={rows?.length}
      caption="Each cited source with the basis of its date and what the KB records about the citation. A link in a source's body is not a recorded passage."
      status={sectionStatus(query)}
      what="citations"
      empty={rows && !rows.length ? "No source recorded. No Sources link." : undefined}
    >
      {() => (
        <div className="ak-table-wrap">
          <table className="ak-table ak-citations">
            <thead>
              <tr>
                <th scope="col">Source</th>
                <th scope="col">Date</th>
                <th scope="col">Citation</th>
                <th scope="col">
                  <span className="sr-only">Open</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {rows?.map((row, i) => {
                const c = row.citation,
                  d = c.source && site?.dates.get(c.source.path);
                return (
                  <tr key={i}>
                    <td>
                      {c.source ? (
                        <Record record={c.source} />
                      ) : (
                        <span className="ak-path">{c.raw || "Source not recorded"}</span>
                      )}
                      <span className="ak-meta">
                        {c.origin ? "Origin citation" : `Sources link #${c.slot}`}
                      </span>
                    </td>
                    <td data-label="Date">
                      {c.source ? (d ? (d.date ?? "No date") : "—") : "—"}
                      {d && <span className="ak-meta">{BASIS[d.basis]}</span>}
                    </td>
                    <td
                      data-label="Citation"
                      className={row.state === "unresolved" ? "text-risk" : undefined}
                    >
                      <span>{inlineState(row)}</span>
                      {c.locator && row.state !== "unresolved" && (
                        <span className="ak-meta ak-path">{c.locator}</span>
                      )}
                    </td>
                    <td>{c.source && <OpenInSource source={c.source} assertion={a.path} />}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  );
}

/** The concepts an assertion belongs to under the shared membership (D1), for its inline facts. */
export const conceptChips = (a: Assertion, data: Knowledge, conceptsOf: Map<string, string[]>) =>
  (conceptsOf.get(a.path) ?? [])
    .map((p) => data.concept.find((c) => c.path === p))
    .filter((c) => !!c)
    .sort((x, y) => (name(x!) < name(y!) ? -1 : 1));
