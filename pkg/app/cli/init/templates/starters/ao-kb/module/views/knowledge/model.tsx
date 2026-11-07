import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, ArrowRight, FileCode, Table2 } from "lucide-react";
import {
  Section,
  SectionBody,
  embedded,
  graphql,
  sectionStatus,
  useViewContext,
} from "@rhizome/kit";
import {
  POSITIONAL,
  badges,
  isPositional,
  isRecord,
  name,
  ownerKey,
  refKey,
  relationships,
} from "./lib/knowledge.ts";
import type { Endpoint, Knowledge, Ref } from "./lib/knowledge.ts";
import type { Edge as KnowledgeEdge } from "./lib/relations.ts";
import {
  CODE_FIELD,
  EVIDENCED_BY,
  codePath,
  declarer,
  declaredEvidence,
  declaringEntry,
  entries,
  fieldRows,
  scalars,
  syncCompare,
} from "./lib/model.ts";
import type {
  FieldLink,
  FieldRow,
  ModelRecord,
  Neighbor,
  SourceValue,
  TypeSpec,
} from "./lib/model.ts";
import { MODEL_WORDS } from "./lib/relations.ts";
import {
  ENDPOINT,
  REF,
  readModelFiles,
  readModelRecords,
  readTypeSpecs,
  useCoverage,
} from "./data.tsx";
import { DateValue, Page, Record, TraceLink, today } from "./parts.tsx";
import { FileLink } from "./evidence.tsx";
import { AOHandoff } from "./record-parts.tsx";

type Edge = { relation: string; target: Endpoint };
type Raw = {
  __typename: string;
  title: string;
  ref: Ref;
  aoValue?: string | null;
  workspace: {
    source: { value?: SourceValue; address?: string } | null;
    fields: { name: string; links: FieldLink[] }[];
    capabilities: { readOnlyReason?: string | null };
    structure: { title: string; ref: Ref }[];
  } | null;
  out: { truncated: boolean; edges: Edge[] };
  in: { truncated: boolean; edges: Edge[] };
};
const basename = (path: string) => path.split("/").pop() ?? path;
/** An authored key as a sentence-case label; the exact key stays in the title. */
const human = (key: string) => key.replace(/[_-]+/g, " ").replace(/^./, (c) => c.toUpperCase());
const shortDate = (value?: string | null) =>
  new Date(Date.parse(value ?? "")).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });

/** The record, its neighbors across native edges, its file, siblings, and code paths. */
async function loadModel(ref: string) {
  const data = await graphql<{ node: Raw | null }>(
    `query ModelRecord($ref: String!) { node(ref: $ref) { __typename title ${REF}
      ... on AOKBModelRecord { aoValue }
      workspace { source capabilities { readOnlyReason } structure { title ${REF} }
        fields { name links { value resolved title ${REF} } } }
      out: neighborhood(direction: OUTBOUND, first: 200) { truncated edges { relation target { ${ENDPOINT} } } }
      in: neighborhood(direction: INBOUND, first: 200) { truncated edges { relation target { ${ENDPOINT} } } } } }`,
    { ref },
  );
  const node = data.node;
  if (!node) return null;
  const edges = [
    ...node.out.edges.map((e) => ({ ...e, out: true, end: ownerKey(e.target) })),
    ...node.in.edges.map((e) => ({ ...e, out: false, end: ownerKey(e.target) })),
  ];
  const self = refKey(node.ref);
  const siblings = (node.workspace?.structure ?? []).filter(
    (s) => s.ref.fragment && s.ref.typeName !== "AOKBModelFile" && refKey(s.ref) !== self,
  );
  const keys = [
    ...edges.flatMap((e) => (isRecord(e.target) ? [e.end] : [])),
    ...siblings.map((s) => refKey(s.ref)),
  ];
  const [records, files] = await Promise.all([
    readModelRecords([...new Set(keys)].slice(0, 200)),
    readModelFiles(),
  ]);
  const raw = entries(node.workspace?.source?.value).find((e) => e.key === CODE_FIELD);
  const code = scalars(raw?.value).map((text) => ({ text, path: codePath(text) }));
  const checked = code.filter((c) => c.path);
  let found: Record<string, { __typename: string } | null> = {};
  let codeError = "";
  try {
    if (checked.length)
      found =
        await graphql(`{ ${checked.map((c, i) => `c${i}: node(ref: ${JSON.stringify(`code:${c.path}`)}) { __typename }`).join("\n")} }`);
  } catch (e) {
    codeError = e instanceof Error ? e.message : String(e);
  }
  return {
    node,
    edges,
    records,
    siblings,
    file: files.find((f) => f.notePath === node.ref.notePath),
    specs: await readTypeSpecs([node.__typename, ...records.map((r) => r.typeName)]),
    code: code.map((c) => ({ ...c, exists: !!c.path && !!found[`c${checked.indexOf(c)}`] })),
    codeError,
  };
}
type Loaded = NonNullable<Awaited<ReturnType<typeof loadModel>>>;

function Rel({
  icon,
  children,
  aside,
  meta,
}: {
  icon: ReactNode;
  children: ReactNode;
  aside?: ReactNode;
  meta?: ReactNode;
}) {
  return (
    <li className="ak-rel">
      <span className="ak-rel-icon" aria-hidden="true">
        {icon}
      </span>
      <span className="ak-rel-main">{children}</span>
      {aside && <span className="ak-rel-aside">{aside}</span>}
      {meta && <span className="ak-rel-meta">{meta}</span>}
    </li>
  );
}
const label = (specs: Record<string, TypeSpec>, type: string) => specs[type]?.label ?? type;

/** Authored values as indented lines, in authored order. */
function lines(value: SourceValue | null | undefined, depth = 0): string[] {
  const pad = "  ".repeat(depth);
  if (!value) return [];
  if (value.kind === "object")
    return (value.entries ?? []).flatMap((e) =>
      e.value.kind === "object" || (e.value.kind === "array" && e.value.items?.length)
        ? [`${pad}${e.key}:`, ...lines(e.value, depth + 1)]
        : [`${pad}${e.key}: ${lines(e.value)[0] ?? ""}`],
    );
  if (value.kind === "array")
    return value.items?.length
      ? value.items.flatMap((item) => {
          const [first, ...rest] = lines(item, depth + 1);
          return [`${pad}- ${(first ?? "").trimStart()}`, ...rest];
        })
      : [`${pad}[]`];
  return [
    `${pad}${value.kind === "null" ? "null" : value.kind === "string" && /^\s|\s$|^$/.test(value.scalar ?? "") ? JSON.stringify(value.scalar) : value.scalar}`,
  ];
}

function Value({ row }: { row: FieldRow }) {
  if (!row.atoms)
    return row.value.kind === "object" || row.value.kind === "array" ? (
      <pre className="ak-value">{lines(row.value).join("\n")}</pre>
    ) : (
      <>{lines(row.value)[0]}</>
    );
  const atom = (a: (typeof row.atoms)[number]) =>
    a.target ? (
      <>
        <Record record={{ ref: a.target.ref, path: a.target.key, title: a.target.title }} />{" "}
        <span className="ak-key">{a.text}</span>
        {a.state === "no edge" && (
          <>
            {" "}
            <span className="ak-state">Checked reference; AO declares no relationship</span>
          </>
        )}
      </>
    ) : (
      <>
        <span className="ak-key">{a.text}</span>{" "}
        <span className={a.state === "wildcard" ? "ak-state" : "ak-state ak-state-risk"}>
          {a.state === "wildcard"
            ? "Wildcard: every record"
            : row.spec?.to.includes("Assertion")
              ? "No assertion with this ID in the selected product"
              : "Not resolved in the selected product: missing, ambiguous, or in another product"}
        </span>
      </>
    );
  return row.value.kind === "array" ? (
    <>
      <ol className="ak-ordered">
        {row.atoms.map((a, i) => (
          <li key={i}>{atom(a)}</li>
        ))}
      </ol>
      {row.atoms.length > 1 && (
        <span className="ak-meta">
          Ordered as authored
          {row.spec?.to.includes("Assertion")
            ? "; each names an assertion (evidenced-by)"
            : row.spec?.edge
              ? `; each names a record (${row.spec.edge.name})`
              : ""}
        </span>
      )}
    </>
  ) : (
    atom(row.atoms[0] ?? { text: "" })
  );
}

function Evidence({
  record,
  loaded,
  data,
  edges,
}: {
  record: ModelRecord;
  loaded: Loaded;
  data: Knowledge;
  edges: KnowledgeEdge[];
}) {
  const spec = loaded.specs[record.typeName];
  const declared = declaredEvidence(record, spec, data.assertion);
  const claims = [...new Set(declared.flatMap((d) => (d.assertion ? [d.assertion] : [])))];
  const coverage = useCoverage(data, claims);
  const dangling = declared.filter((d) => !d.resolved);
  if (!declared.length) return null;
  return (
    <Section
      title="Evidence it rests on"
      count={claims.length + new Set(dangling.map((d) => d.id)).size}
      caption="Claims named in the record's declared evidence, with the origin citation's locator coverage. Coverage is not truth or strength."
      status={sectionStatus(coverage)}
      what="citation states"
    >
      {() => (
        <ul className="ak-rels">
          {claims.map((a) => {
            const sync = syncCompare(a.updatedAt, loaded.file?.synced);
            const repeats = declared.filter((d) => d.assertion === a).length;
            const states = [
              coverage.data?.get(a.path) ?? "Coverage not read",
              ...badges(a, edges, today()).map((b) =>
                b === "Expired" ? "validity ended" : b.toLowerCase(),
              ),
              edges.some((e) => e.rel === "contradicts" && (e.from === a.path || e.to === a.path))
                ? "contradiction recorded"
                : "",
              sync === "later" ? `updated ${shortDate(a.updatedAt)}, after file synced` : "",
              sync === "unknown" ? "update and sync dates not comparable" : "",
              repeats > 1 ? `listed ${repeats} times` : "",
            ].filter(Boolean);
            return (
              <Rel
                key={a.path}
                icon={<ArrowRight size={14} />}
                aside={a.confidence ? human(a.confidence) : "Confidence not recorded"}
                meta={states.join(" · ")}
              >
                {MODEL_WORDS[EVIDENCED_BY][0]} <Record record={a} />
              </Rel>
            );
          })}
          {[...new Set(dangling.map((d) => d.id))].map((id) => (
            <Rel
              key={id}
              icon={<ArrowRight size={14} />}
              aside={<span className="text-risk">Unresolved</span>}
              meta={`No assertion with this ID in the selected product: missing, ambiguous, or in another product. Declared in ${dangling.find((d) => d.id === id)?.field}.`}
            >
              {MODEL_WORDS[EVIDENCED_BY][0]} <span className="ak-key">{id}</span>
            </Rel>
          ))}
        </ul>
      )}
    </Section>
  );
}
function ModelBody({ loaded, data }: { loaded: Loaded; data: Knowledge }) {
  const { node, specs } = loaded;
  const key = refKey(node.ref);
  const record: ModelRecord = {
    key,
    ref: node.ref,
    title: node.title,
    typeName: node.__typename,
    aoValue: node.aoValue,
    value: node.workspace?.source?.value,
    evidence: loaded.edges
      .filter((e) => e.out && e.relation === EVIDENCED_BY)
      .map((e) => e.end),
    evidenceTruncated: node.out.truncated,
    links: Object.fromEntries((node.workspace?.fields ?? []).map((f) => [f.name, f.links])),
  };
  const spec = specs[node.__typename];
  const byKey = new Map(loaded.records.map((r) => [r.key, r]));
  const neighbors: Neighbor[] = loaded.edges.flatMap((e): Neighbor[] => {
    const model = byKey.get(e.end);
    const claim = model ? undefined : data.assertion.find((a) => a.path === e.end);
    const target = model ?? claim;
    if (!target) return [];
    return [
      {
        relation: e.relation,
        out: e.out,
        key: e.end,
        ref: target.ref,
        title: model ? model.title : name(claim!),
        typeName: model ? model.typeName : "Assertion",
        aoValue: model ? model.aoValue : claim!.aoId,
        value: model?.value,
      },
    ];
  });
  // Model edges this record did not author: its own declarations show as Fields links.
  const declaredByOthers = neighbors.filter(
    (n) =>
      n.typeName !== "Assertion" && (n.out ? declarer(record, n) : declarer(n, record)) === n.key,
  );
  // Field links resolve each authored value directly, independent of the capped edge reads.
  const rows = fieldRows(
    record.value,
    spec,
    record.links ?? {},
    new Map(neighbors.map((n) => [n.key, n.title])),
  );
  const address = node.workspace?.source?.address;
  const file = basename(node.ref.notePath);
  const code = loaded.code;
  return (
    <>
      <header className="ak-record-header">
        <div className="ak-record-head">
          <h1>{node.title}</h1>
          <TraceLink record={{ ref: node.ref, path: key, title: node.title }} />
        </div>
        <p className="ak-lead">
          <span>{label(specs, node.__typename)}</span>
          <span className="ak-key">{node.aoValue || "ID not recorded"}</span>
          <span>
            <span className="ak-key">{file}</span>
            {address && <span className="ak-key"> › {address}</span>}
          </span>
          {isPositional(node.ref) && <span>{POSITIONAL}</span>}
        </p>
        {!embedded && node.workspace?.capabilities.readOnlyReason && (
          <p className="ak-footnote">{node.workspace.capabilities.readOnlyReason}</p>
        )}
      </header>
      <div className="ak-record-layout ak-model-layout">
        <div className="ak-record-column">
          <Section title="Fields" empty={rows.length || loaded.file ? undefined : "None recorded"}>
            <dl className="ak-facts">
              {rows.map((row) => (
                <div key={row.key} className="ak-fact">
                  <dt title={row.key}>{human(row.key)}</dt>
                  <dd>
                    <Value row={row} />
                  </dd>
                </div>
              ))}
              <div className="ak-fact">
                <dt>File synced</dt>
                <dd>
                  <DateValue value={loaded.file?.synced} />
                  <span className="ak-meta">
                    {loaded.file
                      ? `synced: in ${file}; one date for every record in the file`
                      : `No model file record for ${file}`}
                  </span>
                </dd>
              </div>
            </dl>
          </Section>
          {declaredByOthers.length > 0 && (
            <Section
              title="Declared by other records"
              count={declaredByOthers.length}
              caption={
                node.in.truncated
                  ? "More records declare relationships to this one than one read returns; this list is incomplete."
                  : undefined
              }
            >
              <ul className="ak-rels">
                {declaredByOthers.map((n) => {
                  const words = MODEL_WORDS[n.relation] ?? [n.relation, `${n.relation} (incoming)`];
                  const field = declaringEntry(n.value, record.aoValue)?.key;
                  return (
                    <Rel
                      key={`${n.relation}-${n.key}`}
                      icon={<ArrowLeft size={14} />}
                      aside={label(specs, n.typeName)}
                      meta={
                        <>
                          <span className="ak-key">
                            {basename(n.ref.notePath)} › {n.aoValue || n.title} ›{" "}
                            {field ?? "authored field not found"}
                          </span>
                          {isPositional(n.ref) && <> · {POSITIONAL}</>}
                        </>
                      }
                    >
                      {words[n.out ? 0 : 1]}{" "}
                      <Record record={{ ref: n.ref, path: n.key, title: n.title }} />
                    </Rel>
                  );
                })}
              </ul>
            </Section>
          )}
          {code.length > 0 && (
            <Section
              title="Referenced code paths"
              count={code.length}
              caption={`Recorded in ${CODE_FIELD}. These are text paths, not model relationships, so Trace does not count them.`}
            >
              <ul className="ak-rels">
                {code.map((c, i) => (
                  <Rel
                    key={i}
                    icon={<FileCode size={14} />}
                    aside={
                      c.exists ? (
                        "In this repository"
                      ) : (
                        <span className="text-muted-foreground">
                          Not available in this repository
                        </span>
                      )
                    }
                    meta={
                      c.exists
                        ? "Exact path found in the configured code roots"
                        : !c.path
                          ? "This path could name a file outside the configured roots, so Rhizome does not open it."
                          : loaded.codeError
                            ? `Could not check this path: ${loaded.codeError}`
                            : "No file at this exact path in the configured code roots. Rhizome does not search elsewhere or guess by file name."
                    }
                  >
                    {c.exists && c.path ? (
                      <FileLink path={c.path}>
                        <span className="ak-key">{c.text}</span>
                      </FileLink>
                    ) : (
                      <span className="ak-key">{c.text}</span>
                    )}
                  </Rel>
                ))}
              </ul>
            </Section>
          )}
          <Evidence record={record} loaded={loaded} data={data} edges={relationships(data).edges} />
        </div>
        <div className="ak-record-column">
          <Section
            title="Source content"
            caption={`${node.ref.notePath}${address ? ` › ${address}` : ""}. This record has its own locator; other rows in the same file are separate records.`}
            controls={<FileLink path={node.ref.notePath}>Open {file}</FileLink>}
          >
            <pre
              className="ak-value ak-source-value"
              aria-label={`Authored value of ${node.title}`}
            >
              {lines(record.value).join("\n")}
            </pre>
          </Section>
          {loaded.siblings.length > 0 && (
            <Section
              title="Same file"
              count={loaded.siblings.length}
              caption={`Other records in ${file}. Each opens as its own record.`}
            >
              <ul className="ak-rels">
                {loaded.siblings.map((s) => {
                  const r = byKey.get(refKey(s.ref));
                  return (
                    <Rel
                      key={refKey(s.ref)}
                      icon={<Table2 size={14} />}
                      aside={<span className="ak-key">{r?.aoValue || "ID not recorded"}</span>}
                      meta={label(specs, s.ref.typeName ?? "")}
                    >
                      <Record
                        record={{ ref: s.ref, path: refKey(s.ref), title: r?.title ?? s.title }}
                      />
                    </Rel>
                  );
                })}
              </ul>
            </Section>
          )}
        </div>
      </div>
      <AOHandoff />
    </>
  );
}

export default function ModelRecordPage() {
  const context = useViewContext();
  const ref = context.kind === "node" ? refKey(context.ref) : "";
  const model = useQuery({
    queryKey: ["ao-kb", "model-record", ref],
    enabled: !!ref,
    queryFn: () => loadModel(ref),
  });
  return (
    <Page>
      {({ data }) => (
        <SectionBody status={sectionStatus(model)} what="model record">
          {() =>
            model.data ? (
              <ModelBody loaded={model.data} data={data} />
            ) : (
              <p>No model record named {ref || "this record"} here</p>
            )
          }
        </SectionBody>
      )}
    </Page>
  );
}
