import { Count, MoreToggle, NodeLink, useTypeColors } from "@rhizome/kit";
import { useEffect, useId, useRef, useState } from "react";
import { MODEL_WORDS, traversal } from "../lib/relations.ts";
import { columns, hiddenTypes } from "./layout.ts";
import { label, presentation, type Trace, type Visit } from "./model.ts";

const verbs: { [key: string]: [string, string] } = {
  cites: ["Cites", "Cited by"],
  "corroborated-by": ["Corroborated by", "Corroborates"],
  "supported-by-claim": ["Supported by", "Provides recorded support to"],
  references: ["References", "Referenced by"],
  contradicts: ["Contradicts", "Contradicted by"],
  supersedes: ["Supersedes", "Superseded by"],
  resolves: ["Resolves", "Resolved by"],
  refines: ["Refines", "Refined by"],
  mentions: ["Mentions", "Mentioned by"],
  "related-concepts": ["Related concept", "Related concept"],
  ...MODEL_WORDS,
};
export function relationship(visit: Visit, trace: Trace) {
  if (!visit.edge) return "Focus · open the record";
  const words = verbs[visit.edge.rel] ?? [visit.edge.rel, `Incoming ${visit.edge.rel} from`];
  const parent =
    visit.parent === trace.focus.path
      ? "the focus"
      : label(trace.nodes.get(visit.parent ?? "")?.record ?? trace.focus);
  return `${words[visit.edge.from === visit.record.path ? 0 : 1]} ${parent}`;
}
function pathToFocus(trace: Trace, active: string | null) {
  const path = new Set<string>();
  while (active && !path.has(active)) {
    path.add(active);
    active = trace.nodes.get(active)?.parent ?? null;
  }
  return path;
}
type Line = { key: string; d: string; from: string; to: string; style: string };
/** Evidence draws solid, model structure dashed, artifact body references dotted. */
const lineStyle = (rel: string) =>
  traversal(rel) === "structure" ? "structure" : rel === "references" ? "reference" : "evidence";
export function Legend() {
  return (
    <p className="ao-trace-legend" aria-label="Line styles">
      {[
        ["evidence", "Evidence"],
        ["structure", "Model structure"],
        ["reference", "Artifact references"],
      ].map(([style, text]) => (
        <span key={style}>
          <svg viewBox="0 0 28 8" aria-hidden="true">
            <line x1="0" y1="4" x2="28" y2="4" data-style={style} />
          </svg>
          {text}
        </span>
      ))}
    </p>
  );
}
export function Graph({
  trace,
  up,
  down,
  expanded,
  onExpand,
  onFocus,
  active,
  onActive,
}: {
  trace: Trace;
  up: number;
  down: number;
  expanded: Set<string>;
  onExpand: (key: string) => void;
  onFocus: (path: string) => void;
  active: string | null;
  onActive: (path: string | null) => void;
}) {
  const layout = columns(trace, up, down, expanded);
  const canvas = useRef<HTMLDivElement>(null);
  const [lines, setLines] = useState<Line[]>([]);
  const marker = useId().replace(/:/g, "");
  const color = useTypeColors();
  const path = pathToFocus(trace, active);
  useEffect(() => {
    const root = canvas.current;
    if (!root) return;
    const measure = () => {
      const box = root.getBoundingClientRect();
      const positions = new Map<string, DOMRect>();
      root
        .querySelectorAll<HTMLElement>("[data-trace-card]")
        .forEach((card) =>
          positions.set(card.dataset.traceCard ?? "", card.getBoundingClientRect()),
        );
      const next: Line[] = [];
      for (const visit of trace.nodes.values()) {
        if (!visit.edge || visit.lane === "related" || !visit.parent) continue;
        const child = positions.get(visit.record.path);
        const parent = positions.get(visit.parent);
        if (!child || !parent) continue;
        const from = visit.edge.from === visit.record.path ? child : parent;
        const to = from === child ? parent : child;
        const right = from.left < to.left;
        const x1 = (right ? from.right : from.left) - box.left;
        const x2 = (right ? to.left : to.right) - box.left;
        const y1 = from.top - box.top + from.height / 2;
        const y2 = to.top - box.top + to.height / 2;
        const mid = (x1 + x2) / 2;
        next.push({
          key: visit.record.path,
          d: `M${x1},${y1} C${mid},${y1} ${mid},${y2} ${x2},${y2}`,
          from: visit.edge.from,
          to: visit.edge.to,
          style: lineStyle(visit.edge.rel),
        });
      }
      setLines(next);
    };
    const observer = new ResizeObserver(measure);
    observer.observe(root);
    root.querySelectorAll("[data-trace-card]").forEach((card) => observer.observe(card));
    measure();
    return () => observer.disconnect();
  }, [trace, up, down, [...expanded].join(",")]);
  const card = (visit: Visit) => {
    const record = visit.record;
    const focused = visit.lane === "focus";
    const content = (
      <>
        <span className="ao-trace-card-meta">
          <span className="ao-trace-type" style={{ background: color(record.__typename) }} />
          {record.__typename}
          <span className="ao-trace-id">{record.aoId || "Not recorded"}</span>
        </span>
        <span className="ao-trace-card-title">{label(record)}</span>
        <span className="ao-trace-card-relation">{relationship(visit, trace)}</span>
      </>
    );
    const props = {
      className: `ao-trace-card${focused ? " ao-trace-focus" : ""}${path.has(record.path) ? " ao-trace-highlight" : ""}`,
      "data-trace-card": record.path,
      onMouseEnter: () => onActive(record.path),
      onMouseLeave: () => onActive(null),
      onFocus: () => onActive(record.path),
      onBlur: () => onActive(null),
    };
    return focused ? (
      <div {...props} key={record.path}>
        {content}
        <NodeLink node={record.ref} view={presentation(record)}>
          Open record
        </NodeLink>
      </div>
    ) : (
      <button
        {...props}
        key={record.path}
        type="button"
        aria-label={`Focus on ${label(record)}. ${relationship(visit, trace)}.`}
        onClick={() => onFocus(record.path)}
      >
        {content}
      </button>
    );
  };
  return (
    <div className="ao-trace-graph-scroll">
      <div
        className="ao-trace-canvas"
        ref={canvas}
        data-graph-ready="true"
        style={{
          gridTemplateColumns: layout.columns
            .map((c) =>
              c.key === "f" || c.all.length ? "minmax(180px, 1fr)" : "minmax(120px, .65fr)",
            )
            .join(" "),
        }}
      >
        <svg className="ao-trace-lines" aria-hidden="true">
          <defs>
            <marker
              id={marker}
              viewBox="0 0 8 8"
              refX="7"
              refY="4"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M0 0 L8 4 L0 8 Z" fill="context-stroke" />
            </marker>
          </defs>
          {lines.map((line) => (
            <path
              key={line.key}
              data-trace-line={line.key}
              d={line.d}
              fill="none"
              markerEnd={`url(#${marker})`}
              data-style={line.style}
              className={path.has(line.from) && path.has(line.to) ? "is-active" : ""}
            />
          ))}
        </svg>
        {layout.columns.map((column) => (
          <section
            className="ao-trace-column"
            key={column.key}
            data-column={column.key}
            aria-label={column.title}
          >
            <h3>
              {column.title}
              <span>
                <Count>{column.shown.length}</Count> of <Count>{column.all.length}</Count>
                {column.key === "f" ? " related" : ""}
              </span>
            </h3>
            <div className="ao-trace-cards">
              {column.key === "f" && card(trace.nodes.get(trace.focus.path)!)}
              {column.shown.map(card)}
              {!column.all.length && column.key !== "f" && (
                <p className="ao-trace-empty">None recorded</p>
              )}
              {(column.hidden.length > 0 ||
                (expanded.has(column.key) && column.shown.length > 6)) && (
                <div className="ao-trace-more">
                  <MoreToggle
                    hidden={column.hidden.length}
                    open={expanded.has(column.key)}
                    onToggle={() => onExpand(column.key)}
                  />
                  {column.hidden.length > 0 && <span>{hiddenTypes(column.hidden)}</span>}
                </div>
              )}
              {column.orphan > 0 && (
                <p className="ao-trace-empty">
                  {column.orphan} more reached through records not shown
                </p>
              )}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}
