import {
  Count,
  DisclosureIcon,
  Fact,
  Facts,
  NodeLink,
  Section,
  Segmented,
  graphql,
  sectionStatus,
  setViewParams,
  useGraphQL,
  useViewParams,
  workspaceViewHref,
  type ViewModuleProps,
} from "@rhizome/kit";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { DIRECTORY_QUERY, createReader, directory, type DirectoryData } from "./trace/data.ts";
import { Graph, Legend } from "./trace/graph.tsx";
import { readTypeSpecs, useBindingFreshness, useModelBindings } from "./data.tsx";
import { ModelFreshness } from "./parts.tsx";
import { columns } from "./trace/layout.ts";
import { depth, label, queryBudget, type Record as KnowledgeRecord } from "./trace/model.ts";
import { ExtractionHistory, Relationships } from "./trace/relationships.tsx";
import { traverse } from "./trace/traverse.ts";
import "./trace/style.css";

const HOPS = [1, 2, 3].map((n) => ({
  value: String(n),
  label: String(n),
  description: `${n} ${n === 1 ? "hop" : "hops"}`,
}));
const KNOWLEDGE = ["Assertion", "Source", "Concept", "Artifact"];
const focusQuery = (model: boolean) => `query TraceFocus($ref: String!) {
  node(ref: $ref) {
    __typename path title ref { notePath kind fragment nodeId typeName structuralFingerprint: structural }
    ... on Assertion { aoId headline }
    ... on Source { aoId headline name }
    ... on Concept { aoId headline name }
    ... on Artifact { aoId headline name }
    ${model ? "... on AOKBModelRecord { aoValue }" : ""}
  }
}`;
function TraceBody({
  records: listedRecords,
  capped,
  configuration,
  model,
}: {
  records: KnowledgeRecord[];
  capped: boolean;
  configuration: ViewModuleProps["configuration"];
  model: boolean;
}) {
  const params = useViewParams();
  const notApplicable = useBindingFreshness().data?.state === "not applicable";
  const focusPath =
    typeof params.focus === "string"
      ? params.focus
      : listedRecords.find((r) => r.__typename === "Assertion")?.path;
  const listedFocus = listedRecords.find((r) => r.path === focusPath);
  const lookup = useGraphQL<{
    node:
      | (Omit<KnowledgeRecord, "aoId"> & { aoId?: string | null; aoValue?: string | null })
      | null;
  }>(focusQuery(model), { ref: focusPath ?? "" }, { enabled: !!focusPath && !listedFocus });
  const found = lookup.data?.node;
  const extra: KnowledgeRecord | undefined =
    found &&
    found.path === focusPath &&
    (KNOWLEDGE.includes(found.__typename) || found.aoValue !== undefined)
      ? { ...found, aoId: found.aoId ?? found.aoValue ?? null }
      : undefined;
  const records = extra && !listedFocus ? [...listedRecords, extra] : listedRecords;
  const focus = listedFocus ?? extra;
  const up = depth(params.up),
    down = depth(params.down);
  const mentions = params.mentions !== false;
  const expanded = new Set(
    typeof params.expand === "string"
      ? params.expand.split(",").filter((key) => /^(f|[ud][1-3])$/.test(key))
      : [],
  );
  const budget = queryBudget(configuration?.queryBudget);
  const [active, setActive] = useState<string | null>(null);
  const result = useQuery({
    queryKey: ["ao-kb-trace", records, focusPath, up, down, mentions, budget, model],
    enabled: !!focus,
    queryFn: async () => {
      if (!focus) throw new Error("Choose a record to trace.");
      const reader = createReader(graphql, records, budget, model);
      const trace = await traverse(focus, { up, down, mentions, budget }, reader.read);
      if (capped)
        trace.stop ??= {
          direction: "the requested scope",
          hop: 0,
          reason: "field",
          detail:
            "The reference directory reached its 200-identities-per-type ceiling; relationships outside that directory may be unavailable.",
        };
      const reached = [...trace.nodes.values()].map((n) => n.record);
      const specs = await readTypeSpecs(
        reached.flatMap((r) => (KNOWLEDGE.includes(r.__typename) ? [] : [r.__typename])),
      );
      for (const r of reached) r.kind = specs[r.__typename]?.label;
      const history = await reader.extraction(focus.path);
      const source = reader.cache.get(focus.path);
      return {
        trace,
        history,
        connectionsRef:
          source?.__typename === "Assertion" ? source.connectionsSection?.ref : undefined,
        extractionTotal:
          source?.__typename === "Source" ? (source.extractedAssertionIds?.length ?? 0) : 0,
      };
    },
  });
  const refocus = (path: string) => {
    setActive(null);
    setViewParams({ focus: path, expand: "" });
  };
  const expand = (key: string) => {
    if (expanded.has(key)) expanded.delete(key);
    else expanded.add(key);
    setViewParams({ expand: [...expanded].join(",") });
  };
  const trace = result.data?.trace;
  const counts = (lane: string) =>
    trace ? [...trace.nodes.values()].filter((n) => n.lane === lane).length : 0;
  const visible = trace ? columns(trace, up, down, expanded).shown.size : 0;
  const href = workspaceViewHref(
    "ao-kb.trace",
    { kind: "group", group: "Knowledge" },
    {
      focus: focusPath ?? "",
      up,
      down,
      expand: [...expanded].join(","),
      rows: params.rows === true,
      mentions,
    },
  );
  return (
    <>
      <div className="ao-trace-toolbar">
        <label className="ao-trace-picker">
          Focus{" "}
          <select
            aria-label="Trace focus"
            value={focusPath ?? ""}
            onChange={(event) => refocus(event.target.value)}
          >
            {!focus && <option value={focusPath ?? ""}>{focusPath || "Choose a record"}</option>}
            {[...KNOWLEDGE, "Product model"].map((type) => {
              const group = records.filter((r) =>
                type === "Product model"
                  ? !KNOWLEDGE.includes(r.__typename)
                  : r.__typename === type,
              );
              return (
                group.length > 0 && (
                  <optgroup label={type} key={type}>
                    {group.map((r) => (
                      <option key={r.path} value={r.path}>
                        {label(r)}
                      </option>
                    ))}
                  </optgroup>
                )
              );
            })}
          </select>
        </label>
        <Segmented
          showLabel
          label="Upstream"
          value={String(up)}
          options={HOPS}
          onChange={(value) => setViewParams({ up: Number(value), expand: "" })}
        />
        <Segmented
          showLabel
          label="Downstream"
          value={String(down)}
          options={HOPS}
          onChange={(value) => setViewParams({ down: Number(value), expand: "" })}
        />
        <label className="ao-trace-mentions">
          <input
            type="checkbox"
            checked={mentions}
            onChange={(event) => setViewParams({ mentions: event.target.checked })}
          />
          Concept mentions
        </label>
        <a className="text-link text-sm" href={href}>
          Link to this trace
        </a>
      </div>
      {capped && (
        <p className="text-sm text-muted-foreground">
          The picker and reference directory contain the first 200 identities per type. References
          beyond that ceiling may remain unresolved; the graph is read separately by hop.
        </p>
      )}
      {!focus && !!focusPath && !listedFocus && (lookup.isPending || lookup.error) ? (
        <Section title="Find the focus" status={sectionStatus(lookup)} what="focus record" />
      ) : !focus ? (
        <p role="status">
          {focusPath
            ? `No record named ${focusPath} here`
            : "No assertions here yet. Choose a source, concept, or artifact to trace."}
        </p>
      ) : (
        <Section title="Relationship navigation" status={sectionStatus(result)} what="trace">
          {() =>
            trace && (
              <>
                <Facts note="Unique records include the focus. Counts cover the requested scope.">
                  <Fact value={trace.nodes.size}>
                    {trace.nodes.size === 1 ? "record" : "records"}
                  </Fact>
                  <Fact value={trace.edges.length}>
                    {trace.edges.length === 1 ? "relationship" : "relationships"}
                  </Fact>
                </Facts>
                <p className="ao-trace-count" aria-live="polite">
                  <Count>{counts("up")}</Count> upstream within {up} {up === 1 ? "hop" : "hops"} ·{" "}
                  <Count>{counts("related")}</Count> related · <Count>{counts("down")}</Count>{" "}
                  downstream within {down} {down === 1 ? "hop" : "hops"}
                  {trace.nodes.size > visible
                    ? ` · ${trace.nodes.size - visible} ${trace.nodes.size - visible === 1 ? "record" : "records"} behind Show more`
                    : ""}
                </p>
                {!model && (
                  <p className="ao-trace-model">
                    {notApplicable
                      ? "Product model bindings are not applicable below schemaVersion 8, so model records are not traced."
                      : "Product model bindings are not ready, so model records are not traced."}
                  </p>
                )}
                {model && <ModelFreshness className="ao-trace-model" />}
                {trace.stop && (
                  <p className="ao-trace-incomplete" role="status">
                    {trace.stop.reason === "budget"
                      ? `Incomplete: the query stopped at its ${budget}-record budget while reading ${trace.stop.direction}${trace.stop.hop === 0 || trace.stop.direction === "related records" ? "" : ` hop ${trace.stop.hop}`}`
                      : `Incomplete: a bounded relationship read stopped while reading ${trace.stop.direction}${trace.stop.hop === 0 || trace.stop.direction === "related records" ? "" : ` hop ${trace.stop.hop}`}. ${trace.stop.detail}`}{" "}
                    Counts cover only what was read. Reduce depth or focus a closer record.
                  </p>
                )}
                <Graph
                  trace={trace}
                  up={up}
                  down={down}
                  expanded={expanded}
                  onExpand={expand}
                  onFocus={refocus}
                  active={active}
                  onActive={setActive}
                />
                <Legend />
                <p className="ao-trace-note">
                  Each drawn line joins a record to the one it was reached from; arrows keep
                  authored direction. Cards name that relationship and refocus this trace. Related
                  records stay beside the focus. Links between boundary records requiring another
                  hop are outside the count.
                </p>
                <Relationships
                  trace={trace}
                  records={[...records, ...[...trace.nodes.values()].map((n) => n.record)]}
                  up={up}
                  down={down}
                  expanded={expanded}
                  open={params.rows === true}
                  onToggle={() => setViewParams({ rows: params.rows !== true })}
                  active={active}
                />
                {focus.__typename === "Source" && (
                  <ExtractionHistory
                    source={focus}
                    rows={result.data?.history ?? []}
                    total={result.data?.extractionTotal ?? 0}
                  />
                )}
                <details className="ao-trace-details">
                  <summary>
                    <DisclosureIcon open={false} />
                    Read limits and declarations
                    {trace.diagnostics.length ? ` · ${trace.diagnostics.length} diagnostics` : ""}
                  </summary>
                  <p>
                    Query budget: {budget} unique record reads, including the focus and candidate
                    owners. Each column displays six records until expanded. The identity directory
                    contains at most 200 records per type and reads no relationships. Depth,
                    expansion, and refocus each add a browser history entry.
                  </p>
                  <p>
                    Only bound declarations participate. Unknown or unresolved body declarations
                    remain inspectable in their authored Connections section; sequence-shaped
                    frontmatter connections are not projected in this slice.
                  </p>
                  {result.data?.connectionsRef && (
                    <NodeLink node={result.data.connectionsRef}>
                      Inspect the focus record’s Connections section
                    </NodeLink>
                  )}
                  {trace.diagnostics.length > 0 && (
                    <ul>
                      {trace.diagnostics.map((message) => (
                        <li className="text-risk" key={message}>
                          {message}
                        </li>
                      ))}
                    </ul>
                  )}
                </details>
              </>
            )
          }
        </Section>
      )}
    </>
  );
}
export default function TracePage({ configuration }: ViewModuleProps) {
  const query = useGraphQL<DirectoryData>(DIRECTORY_QUERY);
  const bindings = useModelBindings();
  return (
    <main className="ao-trace">
      {query.data && !query.error && bindings.data ? (
        <TraceBody
          {...directory(query.data)}
          configuration={configuration}
          model={bindings.data.ready}
        />
      ) : (
        <Section
          title="Trace a record"
          status={sectionStatus(query, bindings)}
          what="knowledge records"
        />
      )}
    </main>
  );
}
