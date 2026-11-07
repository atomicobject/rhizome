import { useEffect, useId, useState } from "react";
import type { ReactNode } from "react";
import {
  Count,
  DisclosureIcon,
  NodeLink,
  Section,
  SectionBody,
  sectionStatus,
  setViewParams,
  useViewParams,
  useViewPreference,
  workspaceViewHref,
  openView,
} from "@rhizome/kit";
import { Button, Input } from "@rhizome/ui";
import { badges, name } from "./lib/knowledge.ts";
import type { Assertion, Knowledge, RecordLink } from "./lib/knowledge.ts";
import type { Edge } from "./lib/relations.ts";
import { useKnowledge } from "./data.tsx";
import { refKey } from "./lib/refs.ts";
import "./knowledge.css";

export const today = () => new Date().toLocaleDateString("en-CA");
export function DateValue({ value }: { value?: string | null }) {
  const time = value ? Date.parse(value) : NaN;
  return (
    <span title={value || undefined}>
      {Number.isFinite(time)
        ? new Date(time).toLocaleDateString("en-US", {
            month: "short",
            day: "numeric",
            year: "numeric",
            timeZone: "UTC",
          })
        : value || "Not recorded"}
    </span>
  );
}
export function Record({ record }: { record: RecordLink }) {
  const view =
    record.ref.typeName === "Assertion"
      ? "ao-kb.assertion"
      : record.ref.typeName === "Source"
        ? "ao-kb.source"
        : undefined;
  return (
    <NodeLink className="link-title" node={record.ref} view={view}>
      {name(record)}
    </NodeLink>
  );
}
export function TraceLink({ record }: { record: RecordLink }) {
  return (
    <a
      className="text-link underline-offset-2 hover:underline"
      href={workspaceViewHref(
        "ao-kb.trace",
        { kind: "group", group: "Knowledge" },
        { focus: refKey(record.ref) },
      )}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey) return;
        e.preventDefault();
        openView(
          "ao-kb.trace",
          { kind: "group", group: "Knowledge" },
          { focus: refKey(record.ref) },
        );
      }}
    >
      Open in Trace
    </a>
  );
}
export function StatusBadges({ a, edges }: { a: Assertion; edges: Edge[] }) {
  return (
    <>
      {badges(a, edges, today()).map((label) => (
        <span key={label} className="ak-status">
          {label}
        </span>
      ))}
    </>
  );
}
export function Field({ label, children }: { label: string; children?: ReactNode }) {
  return (
    <div className="ak-field">
      <dt>{label}</dt>
      <dd>{children || "Not recorded"}</dd>
    </div>
  );
}
export function Page({
  children,
}: {
  children: (state: ReturnType<typeof useKnowledge> & { data: Knowledge }) => ReactNode;
}) {
  const state = useKnowledge();
  return (
    <main className="ak-page">
      <SectionBody status={sectionStatus(state.query)} what="knowledge">
        {() =>
          state.data ? (
            <>
              {state.capped && (
                <p role="alert" className="text-risk">
                  Incomplete: this presentation reads at most 500 records per type. References and
                  observations cover that loaded scope; unresolved IDs may lie outside it.
                </p>
              )}
              {children({ ...state, data: state.data })}
            </>
          ) : null
        }
      </SectionBody>
    </main>
  );
}
export function useFilters() {
  const params = useViewParams();
  return {
    find: typeof params.find === "string" ? params.find : "",
    concept: typeof params.concept === "string" ? params.concept : "",
    sort: typeof params.sort === "string" ? params.sort : "title-asc",
  };
}
export function Filters({ data, concepts = true }: { data: Knowledge; concepts?: boolean }) {
  const { find, concept } = useFilters();
  const conceptId = useId();
  const [draft, setDraft] = useState(find);
  useEffect(() => {
    setDraft(find);
  }, [find]);
  useEffect(() => {
    if (draft === find) return;
    const timer = setTimeout(() => setViewParams({ find: draft || null }), 400);
    return () => clearTimeout(timer);
  }, [draft, find]);
  return (
    <div className="ak-filters">
      <label>
        Find text
        <Input
          value={draft}
          placeholder="Find in knowledge"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") setViewParams({ find: draft || null });
          }}
        />
      </label>
      {concepts && (
        <div className="ak-filter-field">
          <label htmlFor={conceptId}>Concept</label>
          <select
            id={conceptId}
            value={concept}
            onChange={(e) => setViewParams({ concept: e.target.value || null })}
          >
            <option value="">All concepts</option>
            {data.concept.map((c) => (
              <option key={c.path} value={c.path}>
                {name(c)}
              </option>
            ))}
          </select>
        </div>
      )}
      {(find || concept) && <ClearFilters />}
    </div>
  );
}
export function ClearFilters() {
  return (
    <Button size="sm" variant="ghost" onClick={() => setViewParams({ find: null, concept: null })}>
      Clear filters
    </Button>
  );
}
export function Empty() {
  return (
    <p className="ak-empty">
      None match the current filter. <ClearFilters />
    </p>
  );
}
export function Fold({
  title,
  count,
  children,
  defaultOpen = true,
}: {
  title: string;
  count?: number;
  children: ReactNode;
  defaultOpen?: boolean;
}) {
  const pref = useViewPreference(`group.${title}`, {
    defaultValue: defaultOpen,
    validate: (v: unknown): v is boolean => typeof v === "boolean",
  });
  return (
    <section className="ak-fold">
      <button
        className="ak-fold-title"
        aria-expanded={pref.value}
        disabled={pref.loading}
        onClick={() => {
          void pref.set(!pref.value).catch(() => {});
        }}
      >
        <DisclosureIcon open={pref.value} />
        {title}
        {count !== undefined && <Count>{count}</Count>}
      </button>
      {pref.pending && (
        <span role="status" className="text-xs text-muted-foreground">
          Saving display choice…
        </span>
      )}
      {pref.error && (
        <p role="alert">
          {pref.error.message}{" "}
          <Button
            onClick={() => {
              void pref.retry().catch(() => {});
            }}
          >
            Retry
          </Button>
        </p>
      )}
      {pref.value && children}
    </section>
  );
}
export function Basis({ id, children }: { id: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        size="sm"
        variant="ghost"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen(!open)}
      >
        Basis <DisclosureIcon open={open} />
      </Button>
      {open && (
        <p id={id} className="ak-basis">
          {children}
        </p>
      )}
    </>
  );
}
export function LinkList({ title, records }: { title: string; records: RecordLink[] }) {
  return (
    <Section
      title={title}
      count={records.length}
      empty={records.length ? undefined : "None recorded"}
    >
      <ul className="ak-list">
        {records.map((r) => (
          <li key={r.path}>
            <Record record={r} />
          </li>
        ))}
      </ul>
    </Section>
  );
}
