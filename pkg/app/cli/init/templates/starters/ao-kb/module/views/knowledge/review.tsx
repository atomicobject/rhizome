// Review (SPEC-0135 US5): the KB's live gaps with their rules, submissions and findings by
// recorded status, curation registries and reports, and stored confidence, each handed to the AO
// KB skill that acts on it. Counts come from the current index on every load.
import { useEffect, useState } from "react";
import { Count, Section, SectionBody, sectionStatus, useGraphQL, useMarkReady } from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import { GAPS, gap } from "./lib/site.ts";
import type { Root, Site } from "./lib/site.ts";
import { STORED_CONFIDENCE, cmp } from "./lib/kinds.ts";
import { REVIEW_DOCUMENTS_QUERY } from "./lib/queries.ts";
import { Incomplete, ListLink, RefLink, plural, useSite } from "./site/data.tsx";
import { DateValue } from "./parts.tsx";
import "./knowledge.css";

type Doc = Root & {
  status?: string | null;
  recordType?: string | null;
  recordedUpdatedAt?: string | null;
};
type Documents = { submission: Doc[]; curationDocument: Doc[]; conflictRegistry: Doc[] };

function Copy({ command }: { command: string }) {
  const [state, setState] = useState("");
  return (
    <span className="ak-copy">
      <code>{command}</code>
      <Button
        size="sm"
        variant="ghost"
        aria-label={`Copy ${command}`}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(command);
            setState("Copied. Run it in your AO KB agent.");
          } catch {
            setState("Could not copy. Select the command text.");
          }
        }}
      >
        Copy
      </Button>
      {state && (
        <span role="status" className="ak-meta">
          {state}
        </span>
      )}
    </span>
  );
}

function Gaps({ site }: { site: Site }) {
  return (
    <Section
      title="Gaps"
      caption="Structural observations Rhizome can prove from the indexed records, each with its rule. A gap is not a defect, a recommendation, or an AO verdict."
    >
      {GAPS.every((def) => !gap(site, def.id)!.count) && (
        <p className="ak-empty">No gaps by these rules in the current index.</p>
      )}
      <div className="ak-gaps">
        {GAPS.map((def) => {
          const g = gap(site, def.id)!;
          return (
            <div key={def.id} className={`ak-gap${def.sub ? " ak-gap-sub" : ""}`}>
              <span className={`ak-gap-count${g.count ? "" : " ak-zero"}`}>
                {g.count.toLocaleString("en-US")}
              </span>
              <div>
                {g.items.length ? (
                  <ListLink
                    view={`ao-kb.${def.list}`}
                    params={{ gap: def.id }}
                    className="link-title"
                  >
                    {def.label}
                  </ListLink>
                ) : (
                  <span className="ak-gap-label">{def.label}</span>
                )}
                <span className="ak-rule">{def.rule}</span>
                {"values" in g && Object.keys(g.values).length > 0 && (
                  <span className="ak-rule">
                    Recorded values:{" "}
                    {Object.entries(g.values)
                      .sort((a, b) => b[1] - a[1] || cmp(a[0], b[0]))
                      .map(([v, n]) => `${v} ${n}`)
                      .join(", ")}
                  </span>
                )}
              </div>
              {def.command ? (
                <Copy command={def.command} />
              ) : (
                !def.sub && (
                  <span className="ak-meta">
                    AO KB has no command that records a source date; that is a request to AO.
                  </span>
                )
              )}
            </div>
          );
        })}
      </div>
    </Section>
  );
}

const byStatus = (docs: Doc[]) => {
  const groups = new Map<string, Doc[]>();
  for (const d of docs)
    groups.set(d.status ?? "Status not recorded", [
      ...(groups.get(d.status ?? "Status not recorded") ?? []),
      d,
    ]);
  // pending first, then the other recorded statuses by name, each shown verbatim.
  return [...groups].sort(([a], [b]) => +(b === "pending") - +(a === "pending") || cmp(a, b));
};

function DocList({ docs }: { docs: Doc[] }) {
  return (
    <>
      {byStatus(docs).map(([status, rows]) => (
        <div key={status} className="ak-status-group">
          <h3>
            {status} <Count>{rows.length}</Count>
          </h3>
          <ul className="ak-list">
            {rows
              .sort((a, b) => cmp(a.title, b.title) || cmp(a.path, b.path))
              .map((d) => (
                <li key={d.path}>
                  <RefLink record={d} />
                  <span className="ak-path">{d.path}</span>
                </li>
              ))}
          </ul>
        </div>
      ))}
    </>
  );
}

function Documents() {
  const query = useGraphQL<Documents>(REVIEW_DOCUMENTS_QUERY);
  const subs = query.data?.submission ?? [];
  const findings = subs.filter((s) => s.recordType === "finding"),
    plain = subs.filter((s) => s.recordType !== "finding");
  const curation = [
    ...(query.data?.conflictRegistry ?? []),
    ...(query.data?.curationDocument ?? []),
  ].sort((a, b) => cmp(a.path, b.path));
  const pending = plain.filter((s) => s.status === "pending").length;
  return (
    <>
      <Section
        title="Submissions"
        count={query.data ? `${pending} pending` : undefined}
        controls={<Copy command="/ao-kb:suggest-review" />}
        status={sectionStatus(query)}
        what="submissions"
        empty={
          query.data && !plain.length ? "No submissions recorded under _improvements/." : undefined
        }
      >
        {() => <DocList docs={plain} />}
      </Section>
      <Section
        title="Findings"
        status={sectionStatus(query)}
        what="findings"
        empty={
          query.data && !findings.length ? "No findings recorded under _improvements/." : undefined
        }
      >
        {() => <DocList docs={findings} />}
      </Section>
      <p className="ak-footnote">
        Statuses are shown as recorded; the pending count is status pending only. Archived
        submissions (_improvements/_archive/) are not listed.
      </p>
      <Section
        title="Curation registries and reports"
        count={query.data ? curation.length : undefined}
        caption="Every Markdown document directly under .kb/curation/, with its authored updatedAt. A file's modification time is never shown as a curation date."
        status={sectionStatus(query)}
        what="curation documents"
        empty={
          query.data && !curation.length
            ? "No curation documents recorded under .kb/curation/."
            : undefined
        }
      >
        {() => (
          <ul className="ak-list">
            {curation.map((d) => (
              <li key={d.path}>
                <RefLink record={d} />
                <span className="ak-meta">
                  {d.recordedUpdatedAt ? (
                    <>
                      Updated <DateValue value={d.recordedUpdatedAt} />
                    </>
                  ) : (
                    "No recorded date"
                  )}
                </span>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </>
  );
}

function StoredConfidence({ site }: { site: Site }) {
  return (
    <Section
      title="Stored confidence"
      caption="Counts of assertions whose authored confidence is hypothesis or guess. A stored value, not a verdict."
    >
      <p className="ak-stored-counts">
        {STORED_CONFIDENCE.map((value, i) => {
          const n = site.assertions.filter((a) => a.confidence === value).length;
          return (
            <span key={value}>
              {i > 0 && " · "}
              {n ? (
                <ListLink
                  view="ao-kb.assertions"
                  params={{ confidence: value }}
                >{`${value} ${n.toLocaleString("en-US")}`}</ListLink>
              ) : (
                `${value} 0`
              )}
            </span>
          );
        })}{" "}
        <Copy command="/ao-kb:curate" />
      </p>
    </Section>
  );
}

export default function Review() {
  const { query, site } = useSite(),
    markReady = useMarkReady();
  useEffect(() => {
    if (site) markReady();
  }, [site, markReady]);
  return (
    <main className="ak-page ak-site">
      <header className="ak-site-head">
        <h1>Review</h1>
        <p>
          {site
            ? `${plural(site.assertions.length, "assertion")} · ${plural(site.sources.length, "source")} · ${plural(site.concepts.length, "concept")}`
            : "Live gaps and curation"}
        </p>
      </header>
      <SectionBody status={sectionStatus(query)} what="the KB's gaps">
        {() =>
          site && (
            <>
              <Incomplete site={site} />
              <Gaps site={site} />
            </>
          )
        }
      </SectionBody>
      <Documents />
      {site && <StoredConfidence site={site} />}
    </main>
  );
}
