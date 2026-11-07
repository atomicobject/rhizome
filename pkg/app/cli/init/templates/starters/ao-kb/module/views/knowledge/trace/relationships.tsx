import { Count, MoreToggle, NodeLink, Section } from "@rhizome/kit";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@rhizome/ui";
import { columns, edgeRole } from "./layout.ts";
import {
  edgeKey,
  kind,
  label,
  presentation,
  type Record as KnowledgeRecord,
  type Trace,
} from "./model.ts";
import type { Extraction } from "./data.ts";

export function Relationships({
  trace,
  records,
  up,
  down,
  expanded,
  open,
  onToggle,
  active,
}: {
  trace: Trace;
  records: KnowledgeRecord[];
  up: number;
  down: number;
  expanded: Set<string>;
  open: boolean;
  onToggle: () => void;
  active: string | null;
}) {
  const byPath = new Map(records.map((record) => [record.path, record]));
  const layout = columns(trace, up, down, expanded);
  const rows = [...trace.edges, ...trace.listOnly];
  const listOnly = new Set(trace.listOnly.map(edgeKey));
  const cell = (path: string) => {
    const record = byPath.get(path);
    if (!record) return <span>{path}</span>;
    return (
      <>
        <NodeLink className="ao-trace-title" node={record.ref} view={presentation(record)}>
          {label(record)}
        </NodeLink>
        <span className="ao-trace-row-meta">
          {kind(record)} · <span className="ao-trace-id">{record.aoId || "Not recorded"}</span>
        </span>
      </>
    );
  };
  return (
    <Section
      title="Relationships"
      count={trace.edges.length}
      caption={`${trace.edges.length} traversed in scope${trace.listOnly.length ? `; ${trace.listOnly.length} unclassified relationships listed separately below` : ""}. From and To keep authored direction.`}
      empty={rows.length ? undefined : "None recorded"}
    >
      <div className="ao-trace-table-scroll">
        <Table aria-label="Trace relationships">
          <TableHeader>
            <TableRow>
              {["From", "Relationship", "To", "In this trace", "Authored in"].map((title) => (
                <TableHead scope="col" key={title}>
                  {title}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {(open ? rows : rows.slice(0, 10)).map((edge) => (
              <TableRow
                key={edgeKey(edge)}
                className={
                  active && (edge.from === active || edge.to === active)
                    ? "ao-trace-row-highlight"
                    : ""
                }
                data-trace-edge={edgeKey(edge)}
              >
                <TableCell>{cell(edge.from)}</TableCell>
                <TableCell>
                  {edge.rel === "supported-by-claim"
                    ? "Supported by claim"
                    : edge.rel.replace(/-/g, " ")}
                </TableCell>
                <TableCell>{cell(edge.to)}</TableCell>
                <TableCell>
                  {listOnly.has(edgeKey(edge))
                    ? "Unclassified · list only, outside traversal counts"
                    : edgeRole(edge, trace, layout.shown)}
                </TableCell>
                <TableCell>
                  {edge.locations.map((location, i) => (
                    <span
                      className="ao-trace-authored"
                      key={`${location.path}:${location.section}`}
                    >
                      {i > 0 && <span>Also declared in </span>}
                      <NodeLink
                        className="ao-trace-authored-link"
                        node={location.ref ?? byPath.get(location.path)?.ref}
                      >
                        {location.path} › {location.section}
                      </NodeLink>
                    </span>
                  ))}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      {rows.length > 10 && (
        <div className="ao-trace-more">
          <MoreToggle hidden={rows.length - 10} open={open} onToggle={onToggle} />
          <span>{rows.length} relationships available in this list</span>
        </div>
      )}
    </Section>
  );
}

export function ExtractionHistory({
  rows,
  total,
  source,
}: {
  rows: Extraction[];
  total: number;
  source: KnowledgeRecord;
}) {
  return (
    <Section
      title="Extraction history"
      count={total}
      caption={`${label(source)} declares these extracted assertions. Extraction is not evidence of support.`}
      empty={total ? undefined : "None recorded"}
    >
      <ul className="ao-trace-extractions">
        {rows.map((row, index) => (
          <li key={`${row.id}:${index}`}>
            {row.record ? (
              <NodeLink node={row.record.ref} view="ao-kb.assertion">
                {label(row.record)}
              </NodeLink>
            ) : (
              <span className="text-risk">Unresolved or ambiguous id: {row.id}</span>
            )}
            <span className="text-muted-foreground">
              {row.cites === true
                ? "Also cites this source"
                : row.cites === false
                  ? "Does not cite this source"
                  : "Citation not read within the query budget"}
            </span>
          </li>
        ))}
      </ul>
      {total > rows.length && (
        <p className="text-sm text-muted-foreground">
          <Count>{total - rows.length}</Count> more extraction entries beyond the history read cap.
        </p>
      )}
    </Section>
  );
}
