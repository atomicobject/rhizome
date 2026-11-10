import type { MouseEvent } from "react";
import {
  Count,
  Section,
  SectionBody,
  StatusMark,
  sectionStatus,
  usePageNavigation,
  useViewParams,
  workspaceViewHref,
} from "@rhizome/kit";
import { COVERAGE, claimText, name } from "./lib/knowledge.ts";
import type { Assertion, Knowledge } from "./lib/knowledge.ts";
import type { Edge } from "./lib/relations.ts";
import { GAPS, assertionFilter, gap } from "./lib/site.ts";
import type { OAssertion, Site } from "./lib/site.ts";
import { NO_KIND, STORED_LABEL, breakdown } from "./lib/kinds.ts";
import { useCoverage } from "./data.tsx";
import { DateValue, Empty, Filters, Page, Record, StatusBadges, useFilters } from "./parts.tsx";
import { GROUP, useSite } from "./site/data.tsx";

/** US4: one line of counts for the rows shown. Each raw kind and stored-confidence count sets
 * its param and keeps the list's other filters, so the list it opens has that count. */
function Breakdown({ rows }: { rows: OAssertion[] }) {
  const { setViewParams } = usePageNavigation();
  const params = useViewParams();
  const b = breakdown(rows);
  if (!rows.length) return null;
  const link = (patch: Record<string, string>) => ({
    href: workspaceViewHref("ao-kb.assertions", GROUP, { ...params, ...patch }),
    onClick: (e: MouseEvent) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey) return;
      e.preventDefault();
      setViewParams(patch);
    },
  });
  return (
    <p className="ak-breakdown">
      {b.groups.map((g) => (
        <span key={g.id} className="ak-breakdown-group">
          <i
            className="ak-swatch"
            style={{ background: `var(--kind-${g.id})` }}
            aria-hidden="true"
          />
          <b>
            {g.label} {g.count.toLocaleString("en-US")}
          </b>
          :{" "}
          {g.kinds.map((k, i) => (
            <span key={k.kind ?? ""}>
              {i > 0 && ", "}
              {k.kind ? (
                <a className="ak-breakdown-link" {...link({ assertionKind: k.kind })}>
                  {k.kind} {k.count.toLocaleString("en-US")}
                </a>
              ) : (
                `${NO_KIND} ${k.count}`
              )}
            </span>
          ))}
        </span>
      ))}
      <span className="ak-breakdown-group">
        <b>Stored confidence</b>:{" "}
        {b.confidence.map((c, i) => (
          <span key={c.value}>
            {i > 0 && ", "}
            {c.count ? (
              <a className="ak-breakdown-link" {...link({ confidence: c.value })}>
                {c.value} {c.count.toLocaleString("en-US")}
              </a>
            ) : (
              `${c.value} 0`
            )}
          </span>
        ))}
      </span>
    </p>
  );
}

/** What the R2a params other than find and concept narrow the list to. */
function ParamNote({ site }: { site: Site }) {
  const f = useFilters();
  const g = f.gap ? gap(site, f.gap) : undefined;
  const parts = [
    f.assertionKind && `kind ${f.assertionKind}`,
    f.confidence &&
      (f.confidence === "hypothesis,guess"
        ? STORED_LABEL
        : `stored confidence ${f.confidence.split(",").join(" or ")}`),
    f.gap &&
      (g && GAPS.find((d) => d.id === f.gap)?.list === "assertions"
        ? `${g.label}: ${g.rule}`
        : `no assertion gap is named ${f.gap}`),
  ].filter(Boolean);
  return parts.length ? <p className="ak-filter-note">Showing {parts.join(" · ")}</p> : null;
}

const columns = [
  ["title", "Assertion"],
  ["kind", "Kind"],
  ["confidence", "Stored confidence"],
  ["status", "Status"],
  ["sources", "Sources / citation state"],
  ["updatedAt", "Updated"],
  ["priority", "Priority"],
  ["horizon", "Horizon"],
];
function Table({ data, edges, site }: { data: Knowledge; edges: Edge[]; site: Site }) {
  const filters = useFilters(),
    citations = useCoverage(data),
    { setViewParams } = usePageNavigation();
  const [key, direction] = filters.sort.split("-");
  const value = (a: Assertion) => {
    switch (key) {
      case "kind":
        return a.kind || "";
      case "confidence":
        return a.confidence || "";
      case "status":
        return a.status || "";
      case "sources":
        return citations.data?.get(a.path) || "";
      case "updatedAt":
        return a.updatedAt || "";
      case "priority":
        return a.priority || "";
      case "horizon":
        return a.horizon || "";
      default:
        return name(a);
    }
  };
  // The site library's filter, so a matrix cell, a breakdown count, and a gap agree with this list.
  const match = assertionFilter(site, filters),
    text = filters.find.toLocaleLowerCase().trim();
  const of = (a: Assertion) => site.byPath.get(a.path) as OAssertion | undefined;
  const rows = data.assertion
    .filter((a) => {
      const o = of(a);
      return (
        !!o &&
        match(o) &&
        (!text || `${name(a)} ${a.aoId ?? ""} ${claimText(a)}`.toLocaleLowerCase().includes(text))
      );
    })
    .sort(
      (a, b) =>
        String(value(a)).localeCompare(String(value(b))) * (direction === "desc" ? -1 : 1) ||
        a.path.localeCompare(b.path),
    );
  return (
    <>
      <Filters data={data} />
      <ParamNote site={site} />
      <Breakdown rows={rows.map(of).filter((o): o is OAssertion => !!o)} />
      <Section
        title="Assertions"
        count={rows.length}
        caption="Stored AO facts. Locator coverage describes the origin citation, not truth or strength."
        status={sectionStatus(citations)}
        what="citation states"
      >
        {() =>
          rows.length ? (
            <div className="ak-table-wrap">
              <table className="ak-table" role="table">
                <thead role="rowgroup">
                  <tr role="row">
                    {columns.map(([field, label]) => (
                      <th
                        key={field}
                        role="columnheader"
                        scope="col"
                        aria-sort={
                          key === field
                            ? direction === "desc"
                              ? "descending"
                              : "ascending"
                            : "none"
                        }
                      >
                        <button
                          onClick={() =>
                            setViewParams({
                              sort: `${field}-${key === field && direction !== "desc" ? "desc" : "asc"}`,
                            })
                          }
                        >
                          {label}
                          {key === field ? (direction === "desc" ? " ↓" : " ↑") : ""}
                        </button>
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody role="rowgroup">
                  {rows.map((a) => (
                    <tr key={a.path} role="row">
                      <td role="cell">
                        <Record record={a} />
                        <StatusBadges a={a} edges={edges} />
                        <span className="ak-meta">
                          {(site.conceptsOf.get(a.path) ?? [])
                            .map((c) =>
                              name(
                                data.concept.find((v) => v.path === c) ?? {
                                  path: c,
                                  title: c,
                                  ref: { notePath: c, kind: "NOTE" },
                                },
                              ),
                            )
                            .join(" · ") || "In no concept"}
                        </span>
                      </td>
                      <td role="cell" data-label="Kind">
                        {a.kind || "Not recorded"}
                      </td>
                      <td role="cell" data-label="Stored confidence">
                        {a.confidence || "Not recorded"}
                      </td>
                      <td role="cell" data-label="Status">
                        {a.status ? <StatusMark value={a.status} values={[]} /> : "Not recorded"}
                      </td>
                      <td role="cell">
                        <Count>{a.sourcesSection?.citations.length || 0}</Count> Sources{" "}
                        {a.sourcesSection?.citations.length === 1 ? "link" : "links"}
                        <span className="ak-meta">
                          {citations.data?.get(a.path) || "Not recorded"}
                        </span>
                      </td>
                      <td role="cell" data-label="Updated">
                        <DateValue value={a.updatedAt} />
                      </td>
                      <td role="cell" data-label="Priority">
                        {a.priority || "Not recorded"}
                      </td>
                      <td role="cell" data-label="Horizon">
                        {a.horizon || "Not recorded"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <Empty />
          )
        }
      </Section>
      <p className="ak-footnote">
        Coverage states: {COVERAGE.join(" · ")}. Stored confidence is starting confidence set at
        extraction; not a verdict.
      </p>
    </>
  );
}
export default function Assertions() {
  const { query, site } = useSite();
  return (
    <Page>
      {({ data, graph }) => (
        <SectionBody status={sectionStatus(query)} what="kinds and membership">
          {() => site && <Table data={data} edges={graph?.edges || []} site={site} />}
        </SectionBody>
      )}
    </Page>
  );
}
