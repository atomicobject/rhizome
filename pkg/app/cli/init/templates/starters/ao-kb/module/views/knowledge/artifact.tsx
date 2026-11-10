// An artifact (SPEC-0135 US2): its own title and body first, then its recorded support kept apart
// from the other assertions it links, support values that resolve to nothing, and where it sits.
import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Fact,
  Facts,
  RecordPage,
  Section,
  SectionBody,
  sectionStatus,
  useGraphQL,
  useMarkReady,
  useViewContext,
} from "@rhizome/kit";
import { artifactFollowUp, artifactScope } from "./lib/site.ts";
import type { ArtifactRead, Root, Site } from "./lib/site.ts";
import { cmp } from "./lib/kinds.ts";
import { artifactScopeQuery } from "./lib/queries.ts";
import { noteBody } from "./lib/locators.ts";
import {
  AssertionItem,
  Body,
  Incomplete,
  RecordTitle,
  label,
  readCitations,
  useOwnRecord,
  useSite,
} from "./site/data.tsx";
import { ScopedPair } from "./site/scoped.tsx";
import "./knowledge.css";

type Own = Root & { content?: string | null; headline?: string | null };

function Whole({ site, read }: { site: Site; read: ArtifactRead }) {
  const followUp = artifactFollowUp(site, read);
  const reads = useQuery({
    queryKey: ["ao-kb", "artifact-support", read.path, followUp],
    queryFn: ({ signal }) => readCitations(followUp, signal),
  });
  const markReady = useMarkReady();
  useEffect(() => {
    if (reads.data) markReady();
  }, [reads.data, markReady]);
  const scope = artifactScope(site, read, reads.data?.nodes ?? []);
  const title = (p: string) => {
    const r = site.byPath.get(p);
    return r ? label(r) : p;
  };
  const byTitle = (a: string, b: string) => cmp(title(a), title(b)) || cmp(a, b);
  return (
    <>
      <Section
        title="Supported by (recorded)"
        count={scope.supported.length}
        caption="The assertions recorded in supported_by (or the legacy supportingAssertions). Only these are recorded support."
        empty={scope.supported.length ? undefined : "No resolved support recorded."}
      >
        <ul className="ak-list">
          {[...scope.supported].sort(byTitle).map((a) => (
            <AssertionItem key={a} site={site} path={a} />
          ))}
        </ul>
      </Section>
      {scope.unresolved.length > 0 && (
        <Section
          title="Recorded but not resolved"
          count={scope.unresolved.length}
          caption="Support values that match no assertion's AO ID or file name, or match several. Shown as written."
        >
          <ul className="ak-list">
            {scope.unresolved.map((v, i) => (
              <li key={`${v}-${i}`}>
                <code className="ak-path">{v}</code>
              </li>
            ))}
          </ul>
        </Section>
      )}
      <Section
        title="Assertions it links"
        count={scope.links.length}
        caption="Other assertions the artifact links. These are references, not recorded support."
        empty={scope.links.length ? undefined : "None recorded."}
      >
        <ul className="ak-list">
          {[...scope.links].sort(byTitle).map((a) => (
            <AssertionItem key={a} site={site} path={a} />
          ))}
        </ul>
      </Section>
      <Section
        title="Concepts it links"
        count={scope.concepts.length}
        empty={scope.concepts.length ? undefined : "None recorded."}
      >
        <ul className="ak-list">
          {[...scope.concepts].sort(byTitle).map((c) => (
            <li key={c}>
              <RecordTitle site={site} path={c} />
            </li>
          ))}
        </ul>
      </Section>
      <SectionBody status={sectionStatus(reads)} what="the sources its assertions cite">
        {() => (
          <ScopedPair
            site={site}
            scope={{ topics: scope.topics, sources: scope.sources }}
            mapCaption="In scope: the concepts it links and the concepts of the assertions it links or records as support."
          />
        )}
      </SectionBody>
    </>
  );
}

export default function ArtifactPage() {
  const context = useViewContext();
  const path = context.kind === "node" ? context.ref.notePath : "";
  const own = useOwnRecord<Own>(path, "... on Artifact { content aoId name headline }");
  const read = useGraphQL<{ node: ArtifactRead | null }>(artifactScopeQuery(path));
  const { query, site } = useSite();
  const a = own.data?.node;
  return (
    <main className="ak-page ak-site">
      <RecordPage
        className="ak-record"
        kind={<span className="ak-chip">Artifact</span>}
        title={a ? label(a) : path.split("/").pop()?.replace(/\.md$/, "")}
        facts={
          <Facts note={a?.headline && a.headline !== label(a) ? a.headline : undefined}>
            {read.data?.node && (
              <Fact value={read.data.node.supportValues?.length ?? 0}>
                {read.data.node.supportValues?.length === 1
                  ? "recorded support value"
                  : "recorded support values"}
              </Fact>
            )}
          </Facts>
        }
        status={sectionStatus(own)}
        what="this artifact"
        partial={site?.incomplete.length ? <Incomplete site={site} /> : undefined}
      >
        {() =>
          a ? (
            <>
              <Body text={noteBody(a.content ?? "")} site={site} />
              <SectionBody status={sectionStatus(query, read)} what="its links and where it sits">
                {() => site && read.data?.node && <Whole site={site} read={read.data.node} />}
              </SectionBody>
            </>
          ) : (
            <p>No artifact named {path || "this"} here</p>
          )
        }
      </RecordPage>
    </main>
  );
}
