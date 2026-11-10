// Sources by date (SPEC-0135 R4): one row per source folder on the site's one time axis, and the
// one-row strip record pages draw on the same axis. A recorded date is a filled mark, a date in
// the file name a hollow one; area follows resolved extractions; colour needs a strict majority.
import { nodeHref, usePageNavigation } from "@rhizome/kit";
import type { ViewNodeRef } from "@rhizome/kit";
import { folderLabel, sourceMark, stackOffsets, ticks, timeAxis, timeline } from "../lib/site.ts";
import type { Site } from "../lib/site.ts";
import { KIND_GROUPS } from "../lib/kinds.ts";
import { ChartBox, kindColor, useChart } from "./chart.tsx";
import { label, plural } from "./data.tsx";

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const day = (iso: string) => {
  const [y, m, d] = iso.split("-").map(Number);
  return `${MONTHS[m - 1]} ${d}, ${y}`;
};
function tickLabel(t: number, first: boolean, span: number) {
  const d = new Date(t),
    m = MONTHS[d.getUTCMonth()],
    y = d.getUTCFullYear();
  if (span > 900 * 864e5) return String(y);
  if (span <= 120 * 864e5) return `${m} ${d.getUTCDate()}`;
  return first || d.getUTCMonth() === 0 ? `${m} ${y}` : m;
}

export function markTip(site: Site, path: string) {
  const m = sourceMark(site, path),
    d = site.dates.get(path)!;
  const group = m.group
    ? `${KIND_GROUPS.find((g) => g.id === m.group)!.label}, more than half`
    : "mixed or none";
  const r = site.byPath.get(path);
  return `${r ? label(r) : path}\n${day(d.date!)} · ${d.basis === "recorded" ? "Recorded date" : "Date in file name"}\n${
    m.resolved
      ? `${plural(m.resolved, "resolved extracted assertion")} · ${group}`
      : "No resolved extraction"
  }`;
}

function Mark({
  site,
  path,
  cx,
  cy,
  ring,
  scale = 1,
}: {
  site: Site;
  path: string;
  cx: number;
  cy: number;
  ring?: boolean;
  scale?: number;
}) {
  const m = sourceMark(site, path),
    color = kindColor(m.group),
    hollow = m.basis === "file";
  return (
    <>
      <circle className="ak-hit" cx={cx} cy={cy} r={m.r + 5} />
      <circle
        className={hollow ? "ak-mark-file" : "ak-mark-recorded"}
        cx={cx}
        cy={cy}
        r={m.r}
        style={hollow ? { stroke: color } : { fill: color }}
      />
      {ring && <circle className="ak-ring" cx={cx} cy={cy} r={m.r + 3.5} strokeWidth={2 * scale} />}
      <circle className="ak-focus-ring" cx={cx} cy={cy} r={m.r + 3.5} strokeWidth={2} />
    </>
  );
}

function Axis({
  site,
  x,
  y0,
  y1,
  yt,
}: {
  site: Site;
  x: (u: number) => number;
  y0: number;
  y1: number;
  yt: number;
}) {
  const axis = timeAxis(site)!;
  return (
    <g aria-hidden="true">
      {axis.segments.map((m, i) => {
        const px = x(m.u1) - x(m.u0),
          next = axis.segments[i + 1];
        return (
          <g key={i}>
            {ticks(m, px).map((t, j) => (
              <g key={t}>
                <line className="ak-grid" x1={x(axis.u(t))} x2={x(axis.u(t))} y1={y0} y2={y1} />
                <text className="ak-tick" x={x(axis.u(t))} y={yt} textAnchor="middle">
                  {tickLabel(t, j === 0, m.b - m.a)}
                </text>
              </g>
            ))}
            {next && (
              <g className="ak-break">
                <title>Axis break: no sources in between</title>
                <rect
                  x={x(m.u1) + 6}
                  y={y0}
                  width={Math.max(2, x(next.u0) - x(m.u1) - 12)}
                  height={y1 - y0}
                />
              </g>
            )}
          </g>
        );
      })}
    </g>
  );
}

function useOpenSource(site: Site) {
  const { openNode } = usePageNavigation();
  const ref = (path: string) => site.byPath.get(path)?.ref as ViewNodeRef | undefined;
  return {
    href: (path: string) =>
      ref(path) ? nodeHref(ref(path)!, { view: "ao-kb.source" }) : undefined,
    open: (path: string) => {
      const r = ref(path);
      if (r) openNode(r, { view: "ao-kb.source" });
    },
  };
}

const TL_W = 1076,
  LABEL_W = 170,
  ROW_H = 44,
  TOP = 6;
/** The KB overview's chart: every dated source, one row per folder. */
export function Timeline({ site }: { site: Site }) {
  const t = timeline(site),
    axis = timeAxis(site)!;
  const chart = useChart(t.order.length),
    link = useOpenSource(site);
  const k = new Map(t.order.map((p, i) => [p, i]));
  // Drawn at the frame's width (up to the natural width) so labels stay 13px.
  const W = Math.max(480, Math.min(TL_W, chart.width || TL_W));
  const H = TOP + t.rows.length * ROW_H + 30,
    x = (u: number) => LABEL_W + u * (W - LABEL_W - 12);
  return (
    <ChartBox
      chart={chart}
      className="ak-timeline"
      label={`Sources by date, ${plural(site.dated.length, "dated source")} in ${plural(t.rows.length, "folder")}. Arrow keys move by date; Enter opens a source.`}
    >
      <svg viewBox={`0 0 ${W} ${H}`} style={{ maxWidth: W }}>
        <Axis site={site} x={x} y0={TOP} y1={H - 24} yt={H - 6} />
        {t.rows.map((row, ri) => {
          const y = TOP + ri * ROW_H + ROW_H / 2;
          const xs = row.marks.map((p) => ({
            x: x(axis.at(site.dates.get(p)!.date!)),
            r: sourceMark(site, p).r,
          }));
          const off = stackOffsets(xs);
          return (
            <g key={row.folder}>
              <text className="ak-row-label" x={0} y={y + 4}>
                {row.label}
                <title>{folderLabel(row.folder)}</title>
              </text>
              <line className="ak-baseline" x1={LABEL_W} x2={W - 12} y1={y} y2={y} />
              {row.marks.map((p, i) => (
                <a
                  key={p}
                  className="ak-mark"
                  href={link.href(p)}
                  {...chart.item(k.get(p)!, markTip(site, p), () => link.open(p))}
                >
                  <Mark site={site} path={p} cx={xs[i].x} cy={y + off[i]} />
                </a>
              ))}
            </g>
          );
        })}
      </svg>
    </ChartBox>
  );
}

const ST_W = 696,
  ST_L = 26,
  ST_H = 80,
  ST_Y = 34;
/** A record page's strip: its scope's dated sources on the overview's axis, the rest of the KB faint, `ring` ringed. */
export function Strip({ site, dated, ring }: { site: Site; dated: string[]; ring?: string }) {
  const axis = timeAxis(site)!,
    chart = useChart(dated.length),
    link = useOpenSource(site);
  const W = Math.max(320, Math.min(ST_W, chart.width || ST_W));
  const x = (u: number) => ST_L + u * (W - 2 * ST_L);
  const inScope = new Set(dated);
  const xs = dated.map((p) => ({
    x: x(axis.at(site.dates.get(p)!.date!)),
    r: sourceMark(site, p).r,
  }));
  const off = stackOffsets(xs, [0, -8, 8, -15, 15, -21, 21]);
  return (
    <ChartBox
      chart={chart}
      className="ak-strip"
      label={`${plural(dated.length, "dated source")} of ${site.dated.length} in the KB. Arrow keys move by date; Enter opens a source.`}
    >
      <svg viewBox={`0 0 ${W} ${ST_H}`} style={{ maxWidth: W }}>
        <Axis site={site} x={x} y0={4} y1={ST_H - 22} yt={ST_H - 5} />
        <line className="ak-baseline" x1={ST_L} x2={W - ST_L} y1={ST_Y} y2={ST_Y} />
        {site.dated
          .filter((p) => !inScope.has(p))
          .map((p) => (
            <circle
              key={p}
              className="ak-context"
              cx={x(axis.at(site.dates.get(p)!.date!))}
              cy={ST_Y}
              r={2.2}
            />
          ))}
        {dated.map((p, i) => (
          <a
            key={p}
            className="ak-mark"
            href={link.href(p)}
            {...chart.item(i, markTip(site, p), () => link.open(p))}
          >
            <Mark site={site} path={p} cx={xs[i].x} cy={ST_Y + off[i]} ring={p === ring} />
          </a>
        ))}
      </svg>
    </ChartBox>
  );
}

/** What a mark's fill, outline, area, and colour mean. */
export function MarkLegend() {
  return (
    <div className="ak-legend">
      <span>
        <svg className="ak-sym" width="12" height="12" aria-hidden="true">
          <circle cx="6" cy="6" r="5" fill="var(--kind-other)" />
        </svg>
        Recorded date
      </span>
      <span>
        <svg className="ak-sym" width="12" height="12" aria-hidden="true">
          <circle cx="6" cy="6" r="4.5" fill="none" stroke="var(--kind-other)" strokeWidth="1.75" />
        </svg>
        Date in file name
      </span>
      <span>Area: resolved extracted assertions; the smallest mark is none</span>
      <span>Colour: a kind group holding more than half of them; grey: mixed or none</span>
    </div>
  );
}
