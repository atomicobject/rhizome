import { useState } from "react";
import { useDeliveryPortfolio, useDeliveryRecord } from "./data.tsx";
import { useViewContext } from "@rhizome/kit";
import { SourceContent, FieldEvidence } from "./evidence.tsx";
import { ReadState, PromptBox } from "./presentation.tsx";
import type { ReactNode } from "react";
import { Fact, Facts, RecordChip, RecordPage, Section, usePageNavigation } from "@rhizome/kit";
import type { SectionStatus } from "@rhizome/kit";
import type { Effort, Occurrence, RecordSummary } from "./lib/index.ts";
import { identity, promptFor } from "./lib/index.ts";
import { Ready, RecordLink } from "./presentation.tsx";

export type BriefSection = {
  title: string;
  source: RecordSummary | null;
  state: string;
  content: ReactNode;
};
export type EffortProps = {
  record: Effort | null;
  title: string;
  status: SectionStatus;
  governing: readonly Occurrence[];
  approval: ReactNode;
  sections: readonly BriefSection[];
  selectedContent?: (occurrence: Occurrence) => ReactNode;
  entryMentions: readonly Occurrence[];
  logSnapshot: readonly Occurrence[];
  sameScopeAndLog: boolean;
  workflow?: ReactNode;
};

export function Occurrences({
  occurrences,
  render,
}: {
  occurrences: readonly Occurrence[];
  render?: (occurrence: Occurrence) => ReactNode;
}) {
  const nav = usePageNavigation();
  return (
    <ol>
      {occurrences.map((occurrence, index) => (
        <li key={`${occurrence.ordinal}-${index}`}>
          <span>{occurrence.authored || occurrence.link.authoredTarget}</span>
          <div className="ae-note">
            {occurrence.targetKind} · source {occurrence.source.notePath}
            {occurrence.source.fragment ? `#${occurrence.source.fragment}` : ""}
          </div>
          {occurrence.target && (
            <RecordChip
              recordKey={identity(occurrence.target)}
              onOpen={() => occurrence.target && nav.openNode(occurrence.target)}
              wrap
            >
              {occurrence.link.title || occurrence.authored}
            </RecordChip>
          )}
          {render?.(occurrence)}
        </li>
      ))}
    </ol>
  );
}

export function EffortPage({
  record,
  title,
  status,
  governing,
  approval,
  sections,
  selectedContent,
  entryMentions,
  logSnapshot,
  sameScopeAndLog,
  workflow,
}: EffortProps) {
  const checklist = record?.checklist;
  return (
    <main className="ae-page">
      <Ready status={status} />
      <RecordPage
        title={record?.title || title}
        kind={record?.form ?? "Effort"}
        status={status}
        what="this effort"
        partial={
          record && (!record.coverage.complete || record.scope.state !== "known")
            ? "Incomplete execution context. Frozen membership may be unknown; inspect the source evidence below."
            : undefined
        }
        facts={
          <Facts note={record?.status ?? "Status unavailable"}>
            <Fact>{record?.locator ?? title}</Fact>
            <Fact>
              {checklist?.state === "recorded" || checklist?.state === "empty"
                ? `${checklist.checked} of ${checklist.total} checklist items checked`
                : `Checklist ${checklist?.state ?? "unavailable"}`}
            </Fact>
          </Facts>
        }
      >
        {() =>
          record ? (
            <>
              <p className="ae-note">
                This brief presents recorded evidence. Checked items do not establish acceptance or
                change the effort's status.
              </p>
              {workflow}
              <Section title="Plan approval record">{approval}</Section>
              <Section
                title="Governing metadata"
                count={`${governing.length} returned links`}
                caption="Governing links are separate from the frozen selection."
              >
                <Occurrences occurrences={governing} />
                {!governing.length && (
                  <p className="ae-note">No governing links returned in this read.</p>
                )}
              </Section>
              <Section title="Frozen selected scope" caption={`Membership ${record.scope.state}.`}>
                {record.scope.source && (
                  <p className="ae-note">Selected source {record.scope.source.notePath}</p>
                )}
                {record.scope.authored.map((target, index) => (
                  <p key={index} className="ae-note">
                    Recorded selector {target}
                  </p>
                ))}
                {record.scope.reasons.map((reason, index) => (
                  <p role="status" key={index}>
                    {reason}
                  </p>
                ))}
                <h3>Frozen spec occurrences</h3>
                <Occurrences occurrences={record.scope.specs} />
                <h3>Selected story and criterion occurrences</h3>
                <Occurrences occurrences={record.scope.selections} render={selectedContent} />
                {record.scope.state === "known" && !record.scope.selections.length && (
                  <p className="ae-note">
                    The selected source records no selected story or criterion occurrences.
                  </p>
                )}
              </Section>
              {sameScopeAndLog ? (
                <p className="ae-note">The work log and frozen scope select the same source.</p>
              ) : (
                <Section
                  title="Recorded log scope"
                  caption="A separate log snapshot does not establish frozen membership."
                >
                  <Occurrences occurrences={logSnapshot} />
                </Section>
              )}
              {record.form === "EffortWorkspace" && (
                <Section
                  title="Entry mentions"
                  caption="Links from the HTML entry do not supply frozen membership."
                >
                  <Occurrences occurrences={entryMentions} />
                </Section>
              )}
              {sections.map((section, index) => (
                <Section
                  key={`${section.title}-${index}`}
                  title={section.title}
                  caption={`Source state ${section.state}.`}
                >
                  {section.source && <RecordLink record={section.source} />}
                  {section.content}
                </Section>
              ))}
            </>
          ) : (
            <p>This subject did not resolve to a supported effort.</p>
          )
        }
      </RecordPage>
    </main>
  );
}

export default function Page() {
  const context = useViewContext();
  const query = useDeliveryRecord(context.kind === "node" ? context.ref : null);
  const detail = query.data;
  const [selected, setSelected] = useState(false);
  const orientation = useDeliveryPortfolio({ enabled: !!detail?.record });
  const record = detail?.record;
  if (!detail)
    return (
      <main className="ae-page">
        <ReadState query={query} />
        <RecordPage
          kind="Effort"
          title={context.kind === "node" ? context.ref.notePath : "Invalid effort route"}
          status={{ status: query.state === "loading" ? "loading" : "ready" }}
          what="this effort"
        >
          {query.state === "missing-node"
            ? "Effort source unavailable."
            : query.state === "invalid-route"
              ? "Invalid effort route."
              : null}
        </RecordPage>
      </main>
    );
  const log = detail.workLogSnapshot;
  const same =
    record?.kind === "effort" &&
    log &&
    record.scope.source &&
    identity(record.scope.source) === identity(log.ref);
  const approval = detail.node.workspace?.fields.find((field) => field.name === "planApprovedBy");
  const sections = [detail.node, ...detail.components].map((node) => ({
    title: node === detail.node ? "Entry source" : node.title || node.ref.notePath,
    source: null,
    state: node.format || "format unavailable",
    content: (
      <>
        <FieldEvidence node={node} />
        <SourceContent node={node} />
      </>
    ),
  }));
  return (
    <>
      <ReadState query={query} />
      <EffortPage
        record={record?.kind === "effort" ? record : null}
        title={detail.node.title}
        status={{ status: "ready" }}
        workflow={
          record?.kind === "effort" && orientation.data ? (
            <>
              <label>
                <input
                  type="checkbox"
                  checked={selected}
                  onChange={(event) => setSelected(event.target.checked)}
                />{" "}
                Include this effort in the workflow prompt
              </label>
              <PromptBox
                prompt={promptFor(
                  record.status === "planned"
                    ? "planned"
                    : record.status === "active"
                      ? "active"
                      : "history",
                  {
                    ...orientation.data.portfolio,
                    efforts: orientation.data.portfolio.efforts.map((item) =>
                      identity(item.ref) === identity(record.ref) ? record : item,
                    ),
                  },
                  selected ? [identity(record.ref)] : [],
                )}
              />
            </>
          ) : undefined
        }
        governing={detail.governing.occurrences}
        approval={
          !approval ? (
            <p>Approval metadata unavailable in this read.</p>
          ) : approval.present ? (
            <>
              <p>Recorded approval metadata</p>
              {approval.values?.map((value, index) => (
                <p key={index}>{value}</p>
              ))}
            </>
          ) : (
            <p>
              No approver recorded. Absence of metadata does not establish absence of authorization.
            </p>
          )
        }
        sections={sections}
        entryMentions={detail.entryMentions.occurrences}
        logSnapshot={log ? [...log.specs.occurrences, ...log.stories.occurrences] : []}
        sameScopeAndLog={!!same}
        selectedContent={(occurrence) => {
          const target = detail.selectedTargets.find(
            (node) => occurrence.target && identity(node.ref) === identity(occurrence.target),
          );
          return target ? (
            <>
              <p className="ae-note">
                {occurrence.targetKind === "criterion"
                  ? "Criterion-only selection"
                  : occurrence.targetKind === "story"
                    ? "Whole selected story and its criteria"
                    : "Selected source"}
              </p>
              <SourceContent node={target} />
            </>
          ) : (
            <p className="ae-note">Selected target content unavailable.</p>
          );
        }}
      />
    </>
  );
}
