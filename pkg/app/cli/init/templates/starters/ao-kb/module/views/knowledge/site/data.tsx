// The site pages' shared reads and links (SPEC-0135 R5): the orientation query through the
// kit's cached reads, the site library built once per result, and the small pieces every
// page uses to name what it read and where a link goes.
import { useMemo } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  NodeLink,
  readNodes,
  typedRootCoverage,
  useGraphQL,
  usePageNavigation,
  workspaceViewHref,
} from "@rhizome/kit";
import type { ViewNodeRef, ViewParamValue } from "@rhizome/kit";
import { CITATION_SELECTION, ORIENTATION_QUERY } from "../lib/queries.ts";
import { GAPS, buildSite, folderLabel } from "../lib/site.ts";
import type { CitationRead, Orientation, Root, Site } from "../lib/site.ts";
import { makeResolver } from "../lib/relations.ts";
import { NO_KIND, groupOf } from "../lib/kinds.ts";
import { inlineText, paragraphs } from "../lib/prose.ts";
import { readVaultText } from "../lib/passage.ts";
import { BINDING_MANIFESTS } from "../lib/model.ts";
import "./site.css";

export const GROUP = { kind: "group", group: "Knowledge" } as const;

/** The orientation query and the site library over it. `site` is rebuilt only when the data object changes. */
export function useSite() {
  const query = useGraphQL<Orientation>(ORIENTATION_QUERY);
  const site = useMemo(
    () => (query.data ? buildSite(query.data, typedRootCoverage(query.data)) : undefined),
    [query.data],
  );
  return { query, site };
}

/** The Sources-section citations of `refs`, batched by the kit's `readNodes`.
 * ponytail: `readNodes([])` throws in the kit today (it reads the first batch's response when
 * there are none), so an empty scope answers here; drop the guard once the kit returns empty. */
export function readCitations(refs: string[], signal?: AbortSignal) {
  if (!refs.length)
    return Promise.resolve({
      nodes: [] as CitationRead[],
      unresolved: [],
      errors: [],
      truncated: [],
      complete: true,
    });
  return readNodes<CitationRead>(refs, CITATION_SELECTION, { signal });
}

/** The recorded AO profile from the knowledge manifest, and the vault name for a KB with no `_moc.md`. */
export function useKbFacts() {
  return useQuery({
    queryKey: ["ao-kb", "kb-facts"],
    queryFn: async () => {
      let profile: string | undefined;
      try {
        const manifest = JSON.parse((await readVaultText(BINDING_MANIFESTS.knowledge)) ?? "null");
        if (typeof manifest?.context?.profile === "string") profile = manifest.context.profile;
      } catch {
        profile = undefined;
      }
      const status: { vaultName?: unknown } = await fetch("/api/v1/status").then((r) =>
        r.ok ? r.json() : {},
      );
      return {
        profile,
        vault: typeof status?.vaultName === "string" ? status.vaultName : "This KB",
      };
    },
    staleTime: 60_000,
  });
}

export const plural = (n: number, one: string, many = `${one}s`) =>
  `${n.toLocaleString("en-US")} ${n === 1 ? one : many}`;

/** The presentation each record type opens in. Maps of content open as notes. */
export const VIEW_OF: Record<string, string | undefined> = {
  Assertion: "ao-kb.assertion",
  Source: "ao-kb.source",
  Concept: "ao-kb.concept",
  Artifact: "ao-kb.artifact",
};
const refOf = (r: Root) => r.ref as ViewNodeRef | undefined;

type Named = Root & { name?: string | null; headline?: string | null };
/** A record's label: its authored name or headline before the indexed title, which is often a
 * file name. Artifacts put the headline first, because AO writes their names as slugs. */
export const label = (r: Named) =>
  (refOf(r)?.typeName === "Artifact" ? r.headline || r.name : r.name || r.headline) || r.title;

/** A record's title as a link to its page in the presentation its type opens in. */
export function RefLink({
  record,
  className = "link-title",
  tabIndex,
  children,
}: {
  record: Root;
  className?: string;
  tabIndex?: number;
  children?: ReactNode;
}) {
  const ref = refOf(record);
  if (!ref) return <span className="ak-path">{record.path}</span>;
  return (
    <NodeLink
      className={className}
      tabIndex={tabIndex}
      node={ref}
      view={VIEW_OF[ref.typeName ?? ""]}
    >
      {children ?? label(record)}
    </NodeLink>
  );
}

/** A record's title as a link, by path through the site's index. */
export function RecordTitle({
  site,
  path,
  ...rest
}: {
  site: Site;
  path: string;
  className?: string;
  tabIndex?: number;
  children?: ReactNode;
}) {
  const r = site.byPath.get(path);
  return r ? <RefLink record={r} {...rest} /> : <span className="ak-path">{path}</span>;
}

/** A link to one of the Knowledge group's lists with its params; the count it shows is the list's count. */
export function ListLink({
  view,
  params,
  children,
  className = "ak-quiet-link",
  label,
}: {
  view: string;
  params?: Record<string, ViewParamValue>;
  children: ReactNode;
  className?: string;
  label?: string;
}) {
  const { openView } = usePageNavigation();
  return (
    <a
      className={className}
      aria-label={label}
      href={workspaceViewHref(view, GROUP, params)}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        openView(view, GROUP, params);
      }}
    >
      {children}
    </a>
  );
}

/** A link to a gap's records in the list the gap belongs to. */
export const GapLink = ({ id, children }: { id: string; children: ReactNode }) => (
  <ListLink view={`ao-kb.${GAPS.find((g) => g.id === id)!.list}`} params={{ gap: id }}>
    {children}
  </ListLink>
);

/** Roots the orientation query did not read completely, said where their numbers appear (R2c). */
export function Incomplete({ site }: { site: Site }) {
  if (!site.incomplete.length) return null;
  return (
    <p role="alert" className="ak-incomplete">
      Incomplete:{" "}
      {site.incomplete
        .map((r) => `${r.root} read ${r.returned.toLocaleString("en-US")} records (${r.status})`)
        .join("; ")}
      . Counts below cover only what was read.
    </p>
  );
}

/** R4's state for a visual without enough data: what is missing, and where to go instead. */
export function NotEnough({ children }: { children: ReactNode }) {
  return (
    <div className="ak-not-enough" role="note">
      <b>Not enough to draw yet</b> {children}
    </div>
  );
}

/** A resolver over every record the orientation query read, for links in record bodies. */
export function useResolve(site: Site | undefined) {
  return useMemo(
    () =>
      site
        ? makeResolver(
            [...site.byPath.values()].map((r) => ({ key: r.path, aoId: r.aoId, notePath: r.path })),
          )
        : undefined,
    [site],
  );
}

function Inline({
  text,
  site,
  resolve,
}: {
  text: string;
  site?: Site;
  resolve?: (t: string) => string | undefined;
}) {
  // Until the orientation query answers, a link shows as its plain label, never as unresolved.
  return (
    <>
      {inlineText(text, (t) => resolve?.(t)).map((part, i) => {
        if (part.kind === "text") return part.text;
        if (part.kind === "link" && site)
          return (
            <RecordTitle key={i} site={site} path={part.path} className="ak-prose-link">
              {part.label}
            </RecordTitle>
          );
        if (!resolve) return <span key={i}>{part.label}</span>;
        return (
          <span
            key={i}
            className="ak-unresolved"
            title={`Unresolved target: ${part.kind === "unresolved" ? part.target : ""}`}
          >
            {part.label} <span className="ak-unresolved-label">(unresolved)</span>
          </span>
        );
      })}
    </>
  );
}

/** A record's authored body: headings, list items, and paragraphs, with wikilinks to records.
 * `mark` highlights the paragraph at that index of `paragraphs(text)` under `markLabel`. */
export function Body({
  text,
  site,
  mark = -1,
  markLabel,
}: {
  text: string;
  site?: Site;
  mark?: number;
  markLabel?: string;
}) {
  const resolve = useResolve(site);
  const blocks = paragraphs(text);
  if (!blocks.length) return <p className="ak-empty">No body recorded</p>;
  return (
    <div className="ak-body">
      {blocks.map((block, i) => {
        const heading = /^(#{1,6})\s+(.*)$/.exec(block);
        // A leading H1 is the note's title, which the page header already shows.
        if (i === 0 && heading?.[1] === "#" && !block.includes("\n")) return null;
        if (heading && !block.includes("\n")) return <h3 key={i}>{heading[2]}</h3>;
        const lines = block.split("\n");
        const content = lines.every((l) => /^\s*[-*]\s+/.test(l)) ? (
          <ul>
            {lines.map((l, j) => (
              <li key={j}>
                <Inline text={l.replace(/^\s*[-*]\s+/, "")} site={site} resolve={resolve} />
              </li>
            ))}
          </ul>
        ) : (
          <p>
            {lines.map((l, j) => (
              <span key={j}>
                {j > 0 && <br />}
                {/^#{1,6}\s/.test(l) ? (
                  <b>{l.replace(/^#{1,6}\s+/, "")}</b>
                ) : (
                  <Inline text={l} site={site} resolve={resolve} />
                )}
              </span>
            ))}
          </p>
        );
        if (i !== mark) return <div key={i}>{content}</div>;
        return (
          <div key={i} className="ak-body-mark" id="ak-body-mark">
            <span className="ak-body-mark-label">{markLabel}</span>
            {content}
          </div>
        );
      })}
    </div>
  );
}

/** The list page's header: its name, the count it shows, and what it says about it. */
export function ListHead({
  title,
  count,
  about,
}: {
  title: string;
  count: number;
  about?: ReactNode;
}) {
  return (
    <header className="ak-site-head">
      <h1>
        {title} <span className="ak-head-count">{count.toLocaleString("en-US")}</span>
      </h1>
      {about && <p>{about}</p>}
    </header>
  );
}

/** Folder links with counts; the chosen folder is the list's `recordFolder` param. */
export function FolderFilter({
  view,
  folders,
  current,
  label,
  keep,
}: {
  view: string;
  folders: { folder: string; count: number }[];
  current: string;
  label: string;
  keep?: Record<string, string>;
}) {
  return (
    <nav className="ak-folders" aria-label={label}>
      <ListLink view={view} params={keep} className={current ? "ak-folder" : "ak-folder ak-on"}>
        All
      </ListLink>
      {folders.map(({ folder, count }) => (
        <ListLink
          key={folder}
          view={view}
          params={{ ...keep, recordFolder: folder }}
          className={folder === current ? "ak-folder ak-on" : "ak-folder"}
        >
          {folderLabel(folder)} <span className="ak-num">{count}</span>
        </ListLink>
      ))}
    </nav>
  );
}

/** The opened record's own fields and body (`content` goes in the type's fragment, since `Node` has none): read alone, so the page shows it before the orientation query answers (R5). */
export function useOwnRecord<T extends Root & { content?: string | null }>(
  path: string,
  fields: string,
) {
  return useGraphQL<{ node: T | null }>(
    `query Record($ref: String!) { node(ref: $ref) { ref { notePath kind fragment nodeId typeName structuralFingerprint: structural } path title ${fields} } }`,
    { ref: path },
  );
}

/** An assertion in a list: its title, raw kind with the kind group's colour, and stored confidence. */
export function AssertionItem({ site, path }: { site: Site; path: string }) {
  const a = site.byPath.get(path) as
    | (Root & { kind?: string | null; confidence?: string | null })
    | undefined;
  return (
    <li className="ak-assertion-item">
      <RecordTitle site={site} path={path} />
      <span className="ak-meta">
        <i
          className="ak-swatch"
          style={{ background: `var(--kind-${groupOf(a?.kind)})` }}
          aria-hidden="true"
        />
        {a?.kind ?? NO_KIND} · Stored confidence: {a?.confidence ?? "not recorded"}
      </span>
    </li>
  );
}
