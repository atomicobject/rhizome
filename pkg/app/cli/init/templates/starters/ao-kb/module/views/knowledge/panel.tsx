// The Knowledge panel (SPEC-0135 US7): the sidebar panel the Knowledge experience shows while
// it presents the active tab's subject. It stays mounted across subjects, so find text and
// scroll survive navigation; the Recent list lives in sessionStorage, so it also survives the
// panel frame being evicted. Everything it counts comes from the site library.
import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  NodeLink,
  getViewInvocation,
  useGraphQL,
  useMarkReady,
  usePageNavigation,
  useViewActive,
  useViewLocation,
} from "@rhizome/kit";
import type { ViewContext, ViewNodeRef } from "@rhizome/kit";
import {
  drawable,
  folderCounts,
  folderLabel,
  kbTitle,
  layout,
  sourceCandidates,
} from "./lib/site.ts";
import type { ArtifactRead, CitationRead, Link, Root, Scope, Site } from "./lib/site.ts";
import {
  addRecent,
  findByTitle,
  kindCounts,
  locatorBox,
  locatorScope,
  parseRecent,
  recentEntry,
  recentKey,
  titleIndex,
} from "./lib/panel.ts";
import type { RecentEntry } from "./lib/panel.ts";
import { artifactScopeQuery, sourceScopeQuery } from "./lib/queries.ts";
import { cmp } from "./lib/kinds.ts";
import {
  ListLink,
  RefLink,
  VIEW_OF,
  label,
  readCitations,
  useKbFacts,
  useSite,
} from "./site/data.tsx";
import "./panel.css";

// ---- Recent, per window: sessionStorage keyed by vault and experience (R6) ----
function storageKey() {
  const { vaultKey, view } = getViewInvocation();
  return recentKey(vaultKey ?? "", view.experienceKey ?? view.id);
}
function readStored(key: string) {
  try {
    return parseRecent(window.sessionStorage.getItem(key));
  } catch {
    return [];
  }
}

function useRecent(context: ViewContext) {
  const [key] = useState(storageKey);
  const [list, setList] = useState(() => readStored(key));
  // The entry as text, so a new context object for the same subject changes nothing.
  const entry = JSON.stringify(recentEntry(context));
  useEffect(() => {
    // Another frame of this window may have written since; add to what is stored.
    setList(addRecent(readStored(key), JSON.parse(entry) as RecentEntry | null));
  }, [key, entry]);
  useEffect(() => {
    try {
      window.sessionStorage.setItem(key, JSON.stringify(list));
    } catch {
      // Storage refused (private mode or full): Recent lasts as long as this frame.
    }
  }, [key, list]);
  return { list, current: (JSON.parse(entry) as RecentEntry | null)?.key };
}

// ---- The locator (R4's highlight rule in R6's panel colours) ----
const MIN_R = 10;

function LocatorMap({ site, scope }: { site: Site; scope: Scope }) {
  const l = layout(site);
  const box = locatorBox(l.nodes.map((n) => ({ ...n, r: Math.max(n.r, MIN_R) + 6 })))!;
  return (
    <svg
      className="akp-map"
      viewBox={`${box.x} ${box.y} ${box.w} ${box.h}`}
      role="img"
      aria-label={`Topic map with ${scope.topics.size} of ${l.nodes.length} concepts highlighted`}
    >
      {l.edges.map(([a, b]) => {
        const on = scope.topics.has(l.nodes[a].path) && scope.topics.has(l.nodes[b].path);
        return (
          <line
            key={`${a}-${b}`}
            className={on ? "akp-edge akp-on" : "akp-edge"}
            x1={l.nodes[a].x}
            y1={l.nodes[a].y}
            x2={l.nodes[b].x}
            y2={l.nodes[b].y}
          />
        );
      })}
      {l.nodes.map((n) => {
        const on = scope.topics.has(n.path);
        const r = Math.max(n.r, MIN_R);
        return (
          <g key={n.path}>
            <circle
              className={`akp-node${on ? " akp-on" : ""}${n.hollow ? " akp-hollow" : ""}`}
              cx={n.x}
              cy={n.y}
              r={r}
            />
            {scope.mark === n.path && <circle className="akp-ring" cx={n.x} cy={n.y} r={r + 6} />}
          </g>
        );
      })}
    </svg>
  );
}

function Locator({ site, context }: { site: Site; context: ViewContext }) {
  const active = useViewActive();
  const path = context.kind === "node" ? context.ref.notePath : "";
  const isSource = site.dates.has(path);
  const isArtifact = site.artifacts.some((a) => a.path === path);
  // The record's scope read, as its page runs it. An artifact's batched follow-up read only
  // adds sources, and the locator draws concepts, so the panel skips it.
  const source = useGraphQL<{ node: { linkingAssertions: (Link & CitationRead)[] } | null }>(
    sourceScopeQuery(path),
    undefined,
    { enabled: isSource },
  );
  const linking = source.data?.node?.linkingAssertions;
  const extra = linking
    ? sourceCandidates(site, path, linking).filter((p) => !linking.some((l) => l.path === p))
    : [];
  const provenance = useQuery({
    queryKey: ["ao-kb", "source-provenance", path, extra],
    enabled: active && !!linking,
    queryFn: ({ signal }) => readCitations(extra, signal),
  });
  const artifact = useGraphQL<{ node: ArtifactRead | null }>(artifactScopeQuery(path), undefined, {
    enabled: isArtifact,
  });
  const scope = locatorScope(site, context, {
    source: linking && provenance.data ? [...linking, ...provenance.data.nodes] : undefined,
    artifact: artifact.data ? (artifact.data.node ?? { path }) : undefined,
  });
  const ready = scope !== undefined;
  const markReady = useMarkReady();
  useEffect(() => {
    if (ready) markReady();
  }, [ready, markReady]);

  if (scope === null)
    return <p className="akp-note">The locator shows where a record sits on the topic map.</p>;
  if (scope === undefined)
    return source.error || provenance.error || artifact.error ? (
      <p className="akp-note">Could not read this record's scope.</p>
    ) : (
      <p className="akp-note">Reading where this record sits…</p>
    );
  const topics = [...scope.topics].sort(
    (a, b) =>
      cmp(label(site.byPath.get(a)!).toLowerCase(), label(site.byPath.get(b)!).toLowerCase()) ||
      cmp(a, b),
  );
  return (
    <>
      {drawable(site).map ? (
        <LocatorMap site={site} scope={scope} />
      ) : (
        <p className="akp-note">The topic map needs at least 3 concepts.</p>
      )}
      {topics.length ? (
        <ul className="akp-list" aria-label="Concepts in this record's scope">
          {topics.map((p) => (
            <li key={p}>
              <RefLink record={site.byPath.get(p)!} className="akp-row" />
            </li>
          ))}
        </ul>
      ) : (
        <p className="akp-note">No concept is in this record's scope.</p>
      )}
    </>
  );
}

// ---- Find by title ----
function openRecord(nav: ReturnType<typeof usePageNavigation>, r: Root) {
  const ref = r.ref as ViewNodeRef | undefined;
  if (ref) nav.openNode(ref, { view: VIEW_OF[ref.typeName ?? ""] });
}

function Find({ site }: { site: Site }) {
  const nav = usePageNavigation();
  const [text, setText] = useState("");
  const index = useMemo(() => titleIndex(site, label), [site]);
  const matches = findByTitle(index, text);
  const searchAll = () => nav.openSearch({ query: text.trim() });
  return (
    <div className="akp-find">
      <form
        role="search"
        aria-label="Find by title"
        onSubmit={(e) => {
          e.preventDefault();
          if (matches[0]) openRecord(nav, site.byPath.get(matches[0].path)!);
          else if (text.trim()) searchAll();
        }}
      >
        <input
          type="search"
          className="akp-input"
          aria-label="Find by title"
          placeholder="Find by title"
          value={text}
          onChange={(e) => setText(e.target.value)}
        />
      </form>
      {text.trim() && (
        <>
          <ul className="akp-list" aria-label="Title matches">
            {matches.map((m) => (
              <li key={m.path}>
                <RefLink record={site.byPath.get(m.path)!} className="akp-row">
                  <span className="akp-name">{m.label}</span>
                  {m.alias && <span className="akp-aside">also {m.alias}</span>}
                </RefLink>
              </li>
            ))}
          </ul>
          {!matches.length && <p className="akp-note">No title or alias matches.</p>}
          <button type="button" className="akp-row akp-button" onClick={searchAll}>
            <span className="akp-name">Search all text for “{text.trim()}”</span>
          </button>
        </>
      )}
    </div>
  );
}

// ---- The panel ----
function Group({
  title,
  count,
  open,
  children,
}: {
  title: string;
  count?: number;
  open?: boolean;
  children: ReactNode;
}) {
  return (
    <details className="akp-group" open={open}>
      <summary>
        <svg className="akp-caret" viewBox="0 0 16 16" aria-hidden="true">
          <path d="M6 4l4 4-4 4" />
        </svg>
        <span className="akp-name">{title}</span>
        {count !== undefined && <span className="akp-count">{count}</span>}
      </summary>
      {children}
    </details>
  );
}

function Row({ children, count }: { children: ReactNode; count: number }) {
  return (
    <>
      <span className="akp-name">{children}</span>
      <span className="akp-count">{count.toLocaleString("en-US")}</span>
    </>
  );
}

function RecentList({
  site,
  list,
  current,
}: {
  site: Site;
  list: RecentEntry[];
  current?: string;
}) {
  const nav = usePageNavigation();
  if (!list.length) return <p className="akp-note">Records you open in this window appear here.</p>;
  return (
    <ul className="akp-list" aria-label="Recent">
      {list.map((e) => {
        const here = e.key === current ? "page" : undefined;
        if (e.kind !== "node")
          return (
            <li key={e.key}>
              <button
                type="button"
                className="akp-row akp-button"
                aria-current={here}
                onClick={() => nav.openCollection(e.name)}
              >
                <span className="akp-name">{e.name}</span>
                <span className="akp-aside">all records</span>
              </button>
            </li>
          );
        const r = site.byPath.get(e.ref.notePath);
        return (
          <li key={e.key}>
            <NodeLink className="akp-row" node={e.ref} view={VIEW_OF[e.type]} aria-current={here}>
              <span className="akp-name">
                {r ? label(r) : e.ref.notePath.split("/").pop()!.replace(/\.md$/, "")}
              </span>
            </NodeLink>
          </li>
        );
      })}
    </ul>
  );
}

function Contents({ site, context }: { site: Site; context: ViewContext }) {
  const facts = useKbFacts();
  const { list, current } = useRecent(context);
  const concepts = [...site.concepts].sort(
    (a, b) => cmp(label(a).toLowerCase(), label(b).toLowerCase()) || cmp(a.path, b.path),
  );
  const sources = folderCounts(site.sources.map((s) => s.path));
  const artifacts = folderCounts(site.artifacts.map((a) => a.path));
  const kinds = kindCounts(site);
  return (
    <>
      <h1 className="akp-title">{kbTitle(site.mocs, facts.data?.vault ?? "This KB").title}</h1>
      {site.incomplete.length > 0 && (
        <p className="akp-note" role="alert">
          Some records were not read; counts cover only what was.
        </p>
      )}
      <Find site={site} />
      <nav className="akp-list" aria-label="Knowledge pages">
        <ListLink view="ao-kb.overview" className="akp-row">
          KB overview
        </ListLink>
        <ListLink view="ao-kb.review" className="akp-row">
          Review
        </ListLink>
      </nav>
      <section className="akp-section" aria-label="Locator">
        <h2 className="akp-head">Locator</h2>
        <Locator site={site} context={context} />
      </section>
      <section className="akp-section" aria-label="Recent">
        <h2 className="akp-head">Recent</h2>
        <RecentList site={site} list={list} current={current} />
      </section>
      <Group title="Concepts" count={concepts.length}>
        <ul className="akp-list">
          {concepts.map((c) => (
            <li key={c.path}>
              <RefLink record={c} className="akp-row">
                <Row count={site.members.get(c.path)!.length}>{label(c)}</Row>
              </RefLink>
            </li>
          ))}
        </ul>
      </Group>
      <Group title="Source folders" count={sources.length}>
        <ul className="akp-list">
          {sources.map((f) => (
            <li key={f.folder}>
              <ListLink
                view="ao-kb.sources"
                params={{ recordFolder: f.folder }}
                className="akp-row"
                label={`${f.folder || "Vault root"}, ${f.count} sources`}
              >
                <Row count={f.count}>{folderLabel(f.folder)}</Row>
              </ListLink>
            </li>
          ))}
        </ul>
      </Group>
      <Group title="Artifact folders" count={artifacts.length}>
        <ul className="akp-list">
          {artifacts.map((f) => (
            <li key={f.folder}>
              <ListLink
                view="ao-kb.artifacts"
                params={{ recordFolder: f.folder }}
                className="akp-row"
                label={`${f.folder || "Vault root"}, ${f.count} artifacts`}
              >
                <Row count={f.count}>{folderLabel(f.folder)}</Row>
              </ListLink>
            </li>
          ))}
        </ul>
      </Group>
      <Group title="Assertion kinds" count={kinds.length}>
        <ul className="akp-list">
          {kinds.map((k) => (
            <li key={k.kind}>
              <ListLink
                view="ao-kb.assertions"
                params={{ assertionKind: k.kind }}
                className="akp-row"
              >
                <Row count={k.count}>{k.kind}</Row>
              </ListLink>
            </li>
          ))}
        </ul>
      </Group>
    </>
  );
}

export default function KnowledgePanel() {
  const { context } = useViewLocation();
  const { query, site } = useSite();
  return (
    <div className="akp-panel">
      {site ? (
        <Contents site={site} context={context} />
      ) : query.error ? (
        <p className="akp-note" role="alert">
          Could not read the KB: {String((query.error as Error).message ?? query.error)}
        </p>
      ) : (
        <p className="akp-note">Reading the KB…</p>
      )}
    </div>
  );
}
