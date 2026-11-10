import type { LiveStory } from "./data.tsx";
import { useDeliveryPortfolio, useDeliveryRecord } from "./data.tsx";
import { useViewContext } from "@rhizome/kit";
import { SourceContent, FieldEvidence } from "./evidence.tsx";
import { ReadState, WorkflowMap } from "./presentation.tsx";
import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { Fact, Facts, RecordPage, Section, usePageNavigation, useViewParams } from "@rhizome/kit";
import type { SectionStatus } from "@rhizome/kit";
import { identity } from "./lib/index.ts";
import type { Spec, Story } from "./lib/index.ts";
import { Ready } from "./presentation.tsx";

export type SpecProps = {
  record: Spec | null;
  title: string;
  status: SectionStatus;
  narrative: ReactNode;
  storyContent?: (story: Story) => ReactNode;
};

export function InlineStory({
  story,
  focus,
  children,
}: {
  story: Story;
  focus: boolean;
  children?: ReactNode;
}) {
  const details = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    if (!focus || !details.current) return;
    details.current.open = true;
    details.current.scrollIntoView({ block: "nearest" });
    details.current.querySelector("summary")?.focus();
  }, [focus]);
  return (
    <details ref={details}>
      <summary tabIndex={0} role="button">
        <strong>{story.title || story.ref.fragment || "Untitled story"}</strong>{" "}
        <span className="ae-note">
          {story.status ?? "Status unavailable"} ·{" "}
          {story.criteriaCoverage.complete ? "" : "at least "}
          {story.criteria.length} criteria
        </span>
      </summary>
      {children}
      {!story.criteriaCoverage.complete && <p role="status">Criteria read is incomplete.</p>}
      {!children && (
        <ol>
          {story.criteria.map((criterion) => (
            <li key={identity(criterion.ref)}>
              {criterion.title || criterion.locator || "Criterion text unavailable"}
            </li>
          ))}
        </ol>
      )}
      {!story.criteria.length && <p className="ae-note">No criteria returned in this read.</p>}
    </details>
  );
}

export function SpecPage({ record, title, status, narrative, storyContent }: SpecProps) {
  const params = useViewParams();
  const nav = usePageNavigation();
  const storyParam = typeof params.story === "string" ? params.story : null;
  const focused = record?.stories.find(
    (story) =>
      story.locator === storyParam ||
      identity(story.ref) === storyParam ||
      story.ref.nodeId === storyParam ||
      story.ref.fragment === storyParam ||
      story.ref.fragment === `^${storyParam}`,
  );
  return (
    <main className="ae-page">
      <Ready status={status} />
      <RecordPage
        title={record?.title || title}
        kind="Spec"
        status={status}
        what="this spec"
        partial={
          record && (!record.coverage.complete || !record.storiesCoverage.complete)
            ? "Partial contract read. Story and criterion counts describe loaded evidence."
            : undefined
        }
        facts={
          <Facts note={record?.status ?? "Status unavailable"}>
            <Fact>{record?.locator ?? title}</Fact>
          </Facts>
        }
      >
        {() =>
          record ? (
            <>
              {narrative}
              <Section
                title="What done means"
                count={`${record.storiesCoverage.complete ? "" : "at least "}${record.stories.length} stories`}
                caption="Expand a story to inspect its full recorded criteria. Story status is recorded evidence."
              >
                {storyParam && !focused && (
                  <p role="status">
                    The requested inline story was not found in this read.{" "}
                    <button className="ae-link" onClick={() => nav.setViewParams({ story: null })}>
                      Clear story focus
                    </button>
                  </p>
                )}
                {record.stories.map((story) => (
                  <InlineStory key={identity(story.ref)} story={story} focus={story === focused}>
                    {storyContent?.(story)}
                  </InlineStory>
                ))}
                {!record.stories.length && (
                  <p className="ae-note">No stories returned in this read.</p>
                )}
              </Section>
            </>
          ) : (
            <p>This subject did not resolve to a supported spec.</p>
          )
        }
      </RecordPage>
    </main>
  );
}

export default function Page() {
  const context = useViewContext();
  const query = useDeliveryRecord(context.kind === "node" ? context.ref : null);
  const record = query.data?.record;
  const orientation = useDeliveryPortfolio({ enabled: !!record });
  if (!query.data)
    return (
      <main className="ae-page">
        <ReadState query={query} />
        <RecordPage
          kind="Spec"
          title={context.kind === "node" ? context.ref.notePath : "Invalid spec route"}
          status={{ status: query.state === "loading" ? "loading" : "ready" }}
          what="this spec"
        >
          {query.state === "missing-node"
            ? "Spec source unavailable."
            : query.state === "invalid-route"
              ? "Invalid spec route."
              : null}
        </RecordPage>
      </main>
    );
  return (
    <>
      <ReadState query={query} />
      {query.state === "invalid-route" && <p>Invalid spec route.</p>}
      {query.state === "missing-node" && <p>Spec source unavailable.</p>}
      {query.data && (
        <SpecPage
          title={query.data.node.title}
          record={record?.kind === "spec" ? record : null}
          status={{ status: "ready" }}
          narrative={
            <>
              {orientation.data && <WorkflowMap portfolio={orientation.data.portfolio} />}
              <FieldEvidence node={query.data.node} />
              <details>
                <summary>Contract source</summary>
                <SourceContent node={query.data.node} />
              </details>
            </>
          }
          storyContent={(story) => {
            const stories: readonly LiveStory[] = record?.kind === "spec" ? record.stories : [];
            const source = stories.find(
              (item) => identity(item.ref) === identity(story.ref),
            )?.source;
            return source ? <SourceContent node={source} /> : <p>Story source unavailable.</p>;
          }}
        />
      )}
    </>
  );
}
