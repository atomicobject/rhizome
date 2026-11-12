import type { LiveSpec, LiveStory } from "./data.tsx";
import { field } from "./reads.ts";
import { EffortRelationships } from "./relationships.tsx";
import { useDeliveryRecord } from "./data.tsx";
import { useViewContext } from "@rhizome/kit";
import { SourceContent, FieldEvidence } from "./evidence.tsx";
import {
  Notice,
  NoticeMark,
  ReadState,
  Ready,
  RecordedStatus,
  RecordMemberships,
  unit,
} from "./presentation.tsx";
import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import {
  DisclosureIcon,
  Fact,
  Facts,
  RecordPage,
  Section,
  usePageNavigation,
  useViewParams,
} from "@rhizome/kit";
import type { SectionStatus } from "@rhizome/kit";
import { identity } from "./lib/index.ts";
import type { Story } from "./lib/index.ts";

export type SpecProps = {
  record: LiveSpec | null;
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
  story: LiveStory;
  focus: boolean;
  children?: ReactNode;
}) {
  const details = useRef<HTMLDetailsElement>(null);
  const [open, setOpen] = useState(focus);
  useEffect(() => {
    if (!focus || !details.current) return;
    setOpen(true);
    const element = details.current;
    element.scrollIntoView({ block: "nearest" });
    element.dataset.arrived = "";
    const summary = element.querySelector("summary");
    summary?.addEventListener("blur", () => delete element.dataset.arrived, { once: true });
    summary?.focus();
  }, [focus]);
  return (
    <details ref={details} open={open} onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary tabIndex={0} role="button">
        <DisclosureIcon open={false} />
        <strong>{story.title || story.ref.fragment || "Untitled story"}</strong>{" "}
        <span className="ae-summary-facts ae-note">
          <RecordedStatus status={story.status} />{" "}
          <span>
            {story.criteriaCoverage.complete ? "" : "at least "}
            {story.criteria.length} {unit(story.criteria.length, "criteria")}
          </span>
        </span>
      </summary>
      {open && children}
      {!story.criteriaCoverage.complete && (
        <Notice role="status">Criteria read is incomplete.</Notice>
      )}
      {open && !children && (
        <ol>
          {story.criteria.map((criterion) => (
            <li key={identity(criterion.ref)}>
              {criterion.title || criterion.locator || "Criterion text unavailable"}
            </li>
          ))}
        </ol>
      )}
      {!story.criteria.length && (
        <p className="ae-note">
          {!story.criteriaCoverage.complete
            ? "Criterion read unavailable."
            : field(story.source, "acceptanceCriteria")?.present
              ? "The recorded acceptance-criteria section contains no criterion nodes."
              : "No acceptance-criteria section recorded."}
        </p>
      )}
    </details>
  );
}

export function SpecPage({ record, title, status, narrative, storyContent }: SpecProps) {
  const stories: readonly LiveStory[] = record?.stories ?? [];
  const params = useViewParams();
  const nav = usePageNavigation();
  const storyParam = typeof params.story === "string" ? params.story : null;
  const id = record && field(record.source, "id")?.values?.[0];
  const updated = record && field(record.source, "lastUpdated")?.values?.[0];
  const focused = stories.find(
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
          record && (!record.coverage.complete || !record.storiesCoverage.complete) ? (
            <>
              <NoticeMark />
              <span>
                Partial contract read. Story and criterion counts describe loaded evidence.
              </span>
            </>
          ) : undefined
        }
        facts={
          <Facts note={<RecordedStatus status={record?.status} />}>
            {id && (
              <Fact>
                <code className="ae-path">{id}</code>
              </Fact>
            )}
            {updated && <Fact>Updated {updated}</Fact>}
            {record && (
              <Fact>
                <RecordMemberships record={record} />
              </Fact>
            )}
          </Facts>
        }
      >
        {() =>
          record ? (
            <>
              <p>{field(record.source, "summary")?.values?.[0]}</p>
              <Section
                title="What done means"
                count={`${record.storiesCoverage.complete ? "" : "at least "}${record.stories.length} ${unit(record.stories.length, "stories")}`}
              >
                {storyParam && !focused && (
                  <Notice role="status">
                    The requested inline story was not found in this read.{" "}
                    <button className="ae-link" onClick={() => nav.setViewParams({ story: null })}>
                      Clear story focus
                    </button>
                  </Notice>
                )}
                {stories.map((story) => (
                  <InlineStory key={identity(story.ref)} story={story} focus={story === focused}>
                    {storyContent?.(story)}
                  </InlineStory>
                ))}
                {!record.stories.length && (
                  <p className="ae-note">
                    {!record.storiesCoverage.complete
                      ? "Story read unavailable."
                      : field(record.source, "userStories")?.present
                        ? "The recorded user-stories section contains no stories."
                        : "No user-stories section recorded."}
                  </p>
                )}
              </Section>
              <EffortRelationships spec={record} />
              <Section title="Source" className="ae-source-section">
                {narrative}
              </Section>
            </>
          ) : (
            <Notice>This subject did not resolve to a supported spec.</Notice>
          )
        }
      </RecordPage>
    </main>
  );
}

export default function Page() {
  const context = useViewContext();
  const [sourceOpen, setSourceOpen] = useState(false);
  const query = useDeliveryRecord(context.kind === "node" ? context.ref : null);
  const record = query.data?.record;
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
          {query.state === "missing-node" && <Notice>Spec source unavailable.</Notice>}
          {query.state === "invalid-route" && <Notice>Invalid spec route.</Notice>}
        </RecordPage>
      </main>
    );
  return (
    <>
      <ReadState query={query} />
      {query.data && (
        <SpecPage
          title={query.data.node.title}
          record={record?.kind === "spec" ? record : null}
          status={{ status: "ready" }}
          narrative={
            <>
              <FieldEvidence node={query.data.node} />
              <details
                open={sourceOpen}
                onToggle={(event) => setSourceOpen(event.currentTarget.open)}
              >
                <summary>
                  <DisclosureIcon open={false} />
                  Contract source
                </summary>
                {sourceOpen && <SourceContent node={query.data.node} />}
              </details>
            </>
          }
          storyContent={(story) => {
            const stories: readonly LiveStory[] = record?.kind === "spec" ? record.stories : [];
            const source = stories.find(
              (item) => identity(item.ref) === identity(story.ref),
            )?.source;
            const selectedStory = stories.find(
              (item) => identity(item.ref) === identity(story.ref),
            );
            return source && selectedStory ? (
              <>
                <p>{field(source, "summary")?.values?.[0]}</p>
                <ol>
                  {selectedStory.criteria.map((criterion) => (
                    <li key={identity(criterion.ref)}>
                      <SourceContent node={criterion.source} />
                    </li>
                  ))}
                </ol>
              </>
            ) : (
              <p>Story source unavailable.</p>
            );
          }}
        />
      )}
    </>
  );
}
