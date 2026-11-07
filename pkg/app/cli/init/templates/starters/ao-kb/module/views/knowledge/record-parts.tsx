import { useState } from "react";
import { NodeLink, Section, sectionStatus } from "@rhizome/kit";
import { Button } from "@rhizome/ui";
import { useSavedAnalysis } from "./data.tsx";
import { savedAssertion } from "./lib/saved-analysis.ts";
import { claimText, conflictPair, relationships } from "./lib/knowledge.ts";
import type { Assertion, Knowledge } from "./lib/knowledge.ts";
import { collapseContradictions } from "./lib/relations.ts";
import { ClaimProse, LinkedText } from "./prose.tsx";
import { DateValue, Field, Record } from "./parts.tsx";

export function SavedAssessment({ a }: { a: Assertion }) {
  const query = useSavedAnalysis(),
    saved = savedAssertion(query.data, a.aoId || "", a.updatedAt || undefined);
  return (
    <Section title="AO saved assessment" status={sectionStatus(query)} what="saved assessment">
      {() =>
        saved.status === "available" ? (
          <>
            <p>
              {saved.band}
              {saved.mayBeStale && <span className="ak-status">May be stale</span>}
            </p>
            <p className="whitespace-pre-wrap text-sm">{saved.basis}</p>
            <p className="ak-footnote">
              As of {saved.asOf || "Not recorded"} · built {saved.generatedAt || "Not recorded"} ·{" "}
              {saved.builder || "Builder not recorded"} · .kb/index/kb-weights.json
            </p>
            {saved.mayBeStale && <p className="ak-footnote">Claim updated after the as-of date.</p>}
          </>
        ) : (
          <p>
            {saved.status === "unavailable"
              ? "Unavailable: .kb/index/kb-weights.json. This is not a clean result."
              : "Not in snapshot: .kb/index/kb-weights.json."}
          </p>
        )
      }
    </Section>
  );
}
export function AOHandoff() {
  const [copied, setCopied] = useState(""),
    [error, setError] = useState("");
  return (
    <details className="ak-fold">
      <summary className="cursor-pointer">Change this in AO KB</summary>
      <p className="ak-footnote">
        These buttons only copy a command. Run the copied command in your AO KB agent to propose the
        change.
      </p>
      <div className="flex flex-wrap gap-2">
        {["/ao-kb:curate", "/ao-kb:suggest", "/ao-kb:process"].map((command) => (
          <Button
            key={command}
            size="sm"
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(command);
                setCopied(command);
                setError("");
              } catch {
                setError("Could not copy. Select and copy the command text.");
              }
            }}
          >
            Copy {command}
          </Button>
        ))}
      </div>
      {copied && (
        <p role="status" className="text-sm">
          Copied {copied}. No AO KB command was run.
        </p>
      )}
      {error && (
        <p role="alert" className="text-risk">
          {error}
        </p>
      )}
    </details>
  );
}
export function Conflicts({ a, data }: { a: Assertion; data: Knowledge }) {
  const graph = relationships(data);
  const conflicts = data.conflictRegistry
    .flatMap((r) => r.groups.flatMap((g) => g.entries))
    .filter((c) => conflictPair(c, graph.resolve).includes(a.path));
  const contradictions = collapseContradictions(
    graph.edges.filter((e) => e.rel === "contradicts" && (e.from === a.path || e.to === a.path)),
  );
  return (
    <Section
      title="Contradiction recorded"
      count={conflicts.length || contradictions.length}
      empty={!conflicts.length && !contradictions.length ? "None recorded" : undefined}
    >
      {!conflicts.length && contradictions.length > 0 && (
        <p>
          No registry entry recorded. An authored contradiction does not establish an unresolved
          dispute.
        </p>
      )}
      {conflicts.map((c) => (
        <div key={c.ref.fragment || c.title} className="ak-conflict">
          <div className="ak-comparison">
            {conflictPair(c, graph.resolve).map((path) => {
              const claim = data.assertion.find((v) => v.path === path);
              return claim ? (
                <div key={path}>
                  <Record record={claim} />
                  <ClaimProse text={claimText(claim)} graph={graph} />
                </div>
              ) : (
                <p key={path}>Unresolved assertion: {path}</p>
              );
            })}
          </div>
          <div>
            <NodeLink node={c.ref}>{c.title}</NodeLink>
            <dl>
              {c.fields.map((f, i) => (
                <Field key={i} label={f.label.replace(/\*|:$/g, "")}>
                  {f.value.trim() === "—" || !f.value.trim() ? (
                    "Not recorded"
                  ) : f.label === "Winner" && !f.value.includes("[[") && graph.resolve(f.value) ? (
                    <LinkedText text={`[[${f.value}]]`} graph={graph} />
                  ) : (
                    <LinkedText text={f.value} graph={graph} />
                  )}
                </Field>
              ))}
            </dl>
            <p className="ak-footnote">
              From .kb/curation/_conflicts.md. People write Resolution and the human states; AO's
              conflicts script writes the rest. Rhizome does not pick a winner.
            </p>
          </div>
        </div>
      ))}
    </Section>
  );
}
export function History({ a, data }: { a: Assertion; data: Knowledge }) {
  const all = relationships(data).edges.filter((e) => e.rel === "supersedes");
  const reached = new Set([a.path]);
  for (let changed = true; changed;) {
    changed = false;
    for (const e of all)
      if (
        (reached.has(e.from) || reached.has(e.to)) &&
        (!reached.has(e.from) || !reached.has(e.to))
      ) {
        reached.add(e.from);
        reached.add(e.to);
        changed = true;
      }
  }
  const chain = all.filter((e) => reached.has(e.from) && reached.has(e.to));
  return (
    <Section
      title="History"
      caption="Authored supersession direction. Validity dates remain separate recorded facts."
    >
      <p className="text-sm">
        Valid from <DateValue value={a.validFrom} /> · Valid until{" "}
        <DateValue value={a.validUntil} />
      </p>
      {!chain.length ? (
        <p className="ak-footnote">No supersession recorded.</p>
      ) : (
        <ul className="ak-list">
          {chain.map((e) => {
            const from = data.assertion.find((v) => v.path === e.from),
              to = data.assertion.find((v) => v.path === e.to);
            return (
              <li key={`${e.from}-${e.to}`}>
                {from && <Record record={from} />}{" "}
                <span className="text-muted-foreground">supersedes</span>{" "}
                {to && <Record record={to} />}
                <span className="ak-meta">
                  Successor valid from <DateValue value={from?.validFrom} /> · predecessor valid
                  until <DateValue value={to?.validUntil} />
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </Section>
  );
}
