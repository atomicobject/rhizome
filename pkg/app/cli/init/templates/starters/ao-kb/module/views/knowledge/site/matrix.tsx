// The topic-by-kind matrix (SPEC-0135 R4, US4): ARIA grid, one tab stop, arrow keys by cell, Enter
// or Space opens the Assertions list with the cell's params, so the list's count is the cell's.
import type { KeyboardEvent } from "react";
import { usePageNavigation, workspaceViewHref } from "@rhizome/kit";
import { matrix, tintPercent } from "../lib/site.ts";
import type { Site } from "../lib/site.ts";
import { STORED_CONFIDENCE, STORED_LABEL } from "../lib/kinds.ts";
import { kindColor, useChart } from "./chart.tsx";
import { GROUP, RecordTitle, label, plural } from "./data.tsx";

const STORED = STORED_CONFIDENCE.join(",");
/** Ink numerals on a tint capped at 60% in light and 50% in dark (R4, R8). */
const fill = (color: string, n: number, max: number) =>
  `light-dark(color-mix(in srgb, ${color} ${tintPercent(n, max, 60)}%, var(--surface)), color-mix(in srgb, ${color} ${tintPercent(n, max, 50)}%, var(--surface)))`;

export function Matrix({ site }: { site: Site }) {
  const m = matrix(site),
    { openView, openNode } = usePageNavigation();
  const width = m.columns.length + 2; // the row header, each kind, stored confidence
  const chart = useChart(m.rows.length * width);
  const title = (c: string) => {
    const r = site.byPath.get(c);
    return r ? label(r) : c;
  };

  const onKeyDown = (e: KeyboardEvent) => {
    const at = chart.active,
      r = Math.floor(at / width),
      c = at % width;
    const to = (
      {
        ArrowRight: c < width - 1 ? at + 1 : at,
        ArrowLeft: c > 0 ? at - 1 : at,
        ArrowDown: r < m.rows.length - 1 ? at + width : at,
        ArrowUp: r > 0 ? at - width : at,
        Home: e.ctrlKey ? 0 : r * width,
        End: e.ctrlKey ? m.rows.length * width - 1 : r * width + width - 1,
      } as Record<string, number>
    )[e.key];
    if (to !== undefined) {
      e.preventDefault();
      chart.move(to);
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      (e.target as HTMLElement).click();
    }
  };

  const cell = (
    i: number,
    n: number,
    concept: string,
    label: string,
    params: Record<string, string>,
    color: string,
    extra = "",
  ) => {
    const tip = `${title(concept)} · ${label}\n${n ? plural(n, "assertion") : "None"}`;
    const props = chart.item(
      i,
      tip,
      n ? () => openView("ao-kb.assertions", GROUP, params) : undefined,
    );
    return (
      <td
        key={i}
        role="gridcell"
        className={`${n ? "" : "ak-zero "}${extra}`}
        style={n ? { background: fill(color, n, m.max) } : undefined}
        {...props}
      >
        {n ? (
          <a href={workspaceViewHref("ao-kb.assertions", GROUP, params)} tabIndex={-1}>
            {n}
          </a>
        ) : null}
      </td>
    );
  };

  return (
    <div ref={chart.box} className="ak-matrix-wrap">
      <table
        className="ak-matrix"
        role="grid"
        aria-label="Concepts by assertion kind. Arrow keys move between cells; Enter opens the list."
        onKeyDown={onKeyDown}
      >
        <thead>
          <tr role="row">
            <th role="columnheader" aria-hidden="true" />
            {m.groups.map((g) => (
              <th
                key={g.id}
                role="columnheader"
                colSpan={g.span}
                className="ak-group-head"
                style={{ borderBottomColor: kindColor(g.id) }}
              >
                {g.label}
              </th>
            ))}
            <th role="columnheader" className="ak-stored-gap" aria-hidden="true" />
          </tr>
          <tr role="row">
            <th role="columnheader" scope="col" className="ak-row-head">
              Concept
            </th>
            {m.columns.map((col, i) => (
              <th key={col.kind} role="columnheader" scope="col">
                {col.kind}
                <span className="ak-total" aria-label={`${m.totals[i]} assertions in all`}>
                  {m.totals[i]}
                </span>
              </th>
            ))}
            <th role="columnheader" scope="col" className="ak-stored">
              {STORED_LABEL}
              <span className="ak-total" aria-label={`${m.storedTotal} assertions in all`}>
                {m.storedTotal}
              </span>
            </th>
          </tr>
        </thead>
        <tbody>
          {m.rows.map((row, r) => {
            const base = r * width;
            const head = chart.item(base, `${title(row.concept)}\nOpen the concept`, () => {
              const ref = site.byPath.get(row.concept)?.ref;
              if (ref) openNode(ref as never, { view: "ao-kb.concept" });
            });
            return (
              <tr key={row.concept} role="row">
                <th role="rowheader" scope="row" {...head}>
                  <RecordTitle
                    site={site}
                    path={row.concept}
                    className="link-title"
                    tabIndex={-1}
                  />
                </th>
                {row.cells.map((n, c) =>
                  cell(
                    base + 1 + c,
                    n,
                    row.concept,
                    m.columns[c].kind,
                    { concept: row.concept, assertionKind: m.columns[c].kind },
                    kindColor(m.columns[c].group),
                  ),
                )}
                {cell(
                  base + width - 1,
                  row.stored,
                  row.concept,
                  STORED_LABEL,
                  { concept: row.concept, confidence: STORED },
                  kindColor("other"),
                  "ak-stored",
                )}
              </tr>
            );
          })}
        </tbody>
      </table>
      {chart.tip && (
        <div className="ak-tip" role="tooltip" style={{ left: chart.tip.x, top: chart.tip.y }}>
          {chart.tip.text}
        </div>
      )}
    </div>
  );
}
