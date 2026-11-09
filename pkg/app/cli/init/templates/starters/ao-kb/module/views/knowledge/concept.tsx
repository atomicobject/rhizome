// A concept (SPEC-0135 US2): its own title, headline, and body first, then where it sits on the
// KB's map and time axis, and what the KB says about it, grouped by kind group.
import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Fact,
  Facts,
  RecordPage,
  Section,
  SectionBody,
  sectionStatus,
  useMarkReady,
  useViewContext,
} from "@rhizome/kit";
import { byKindGroup, conceptScope } from "./lib/site.ts";
import type { Root, Site } from "./lib/site.ts";
import { noteBody } from "./lib/locators.ts";
import {
  AssertionItem,
  Body,
  Incomplete,
  ListLink,
  RecordTitle,
  label,
  plural,
  readCitations,
  useOwnRecord,
  useSite,
} from "./site/data.tsx";
import { ScopedPair } from "./site/scoped.tsx";
import "./knowledge.css";

type Own = Root & { content?: string | null; headline?: string | null; aliases?: string[] | null };

function Holdings({ site, path }: { site: Site; path: string }) {
  const members = site.members.get(path) ?? [];
  const groups = byKindGroup(site, members);
  return (
    <Section
      title="What the KB says about it"
      count={members.length}
      caption="Assertions that link this concept or that it links. Many are hypotheses or guesses; each shows its stored confidence."
      empty={members.length ? undefined : "No member assertions recorded."}
    >
      {groups.map((g) => (
        <div key={g.id} className="ak-kind-group">
          <h3>
            {g.label} <span className="ak-num">{g.items.length}</span>
          </h3>
          <ul className="ak-list">
            {g.items.map((a) => (
              <AssertionItem key={a} site={site} path={a} />
            ))}
          </ul>
        </div>
      ))}
      {members.length > 0 && (
        <p className="ak-footnote">
          <ListLink view="ao-kb.assertions" params={{ concept: path }}>
            List these {plural(members.length, "assertion")}
          </ListLink>{" "}
          with their breakdown and filters.
        </p>
      )}
    </Section>
  );
}

function Whole({ site, path }: { site: Site; path: string }) {
  const members = site.members.get(path) ?? [];
  const reads = useQuery({
    queryKey: ["ao-kb", "concept-scope", path, members],
    queryFn: ({ signal }) => readCitations(members, signal),
  });
  const related = site.related.get(path) ?? [];
  const markReady = useMarkReady();
  useEffect(() => {
    if (reads.data) markReady();
  }, [reads.data, markReady]);
  return (
    <>
      <SectionBody status={sectionStatus(reads)} what="the sources its assertions cite">
        {() =>
          reads.data && (
            <>
              {!reads.data.complete && (
                <p role="alert" className="ak-incomplete">
                  Incomplete: {plural(reads.data.unresolved.length, "member")} did not resolve and{" "}
                  {reads.data.errors.length} reads failed, so some cited sources may be missing.
                </p>
              )}
              <ScopedPair
                site={site}
                scope={conceptScope(site, path, reads.data.nodes)}
                mapCaption="Ringed: this concept. In scope: it and the concepts recorded in its Related Concepts."
              />
            </>
          )
        }
      </SectionBody>
      <Holdings site={site} path={path} />
      <Section
        title="Related Concepts"
        count={related.length}
        empty={related.length ? undefined : "None recorded in its Related Concepts section."}
      >
        <ul className="ak-list">
          {related.map((c) => (
            <li key={c}>
              <RecordTitle site={site} path={c} />
            </li>
          ))}
        </ul>
      </Section>
    </>
  );
}

export default function ConceptPage() {
  const context = useViewContext();
  const path = context.kind === "node" ? context.ref.notePath : "";
  const own = useOwnRecord<Own>(path, "... on Concept { content aoId name headline aliases }");
  const { query, site } = useSite();
  const c = own.data?.node;
  const members = site?.members.get(path)?.length;
  return (
    <main className="ak-page ak-site">
      <RecordPage
        className="ak-record"
        kind={<span className="ak-chip">Concept</span>}
        title={c ? label(c) : path.split("/").pop()?.replace(/\.md$/, "")}
        facts={
          <Facts note={c?.headline && c.headline !== label(c) ? c.headline : undefined}>
            {members !== undefined && (
              <Fact value={members}>
                {members === 1 ? "member assertion" : "member assertions"}
              </Fact>
            )}
            {c?.aliases?.length ? <Fact>Also called {c.aliases.join(", ")}</Fact> : null}
          </Facts>
        }
        status={sectionStatus(own)}
        what="this concept"
        partial={site?.incomplete.length ? <Incomplete site={site} /> : undefined}
      >
        {() =>
          c ? (
            <>
              <Body text={noteBody(c.content ?? "")} site={site} />
              <SectionBody status={sectionStatus(query)} what="where it sits in the KB">
                {() => site && <Whole site={site} path={path} />}
              </SectionBody>
            </>
          ) : (
            <p>No concept named {path || "this"} here</p>
          )
        }
      </RecordPage>
    </main>
  );
}
