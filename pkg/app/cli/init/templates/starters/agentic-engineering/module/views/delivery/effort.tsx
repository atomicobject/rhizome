import { useEffect, useState } from "react";
import {
  Fact,
  Facts,
  RecordChip,
  RecordPage,
  Section,
  usePageNavigation,
  useViewContext,
} from "@rhizome/kit";
import { useDeliveryRecord } from "./data.tsx";
import type { LiveNode, RecordDetail } from "./data.tsx";
import { field } from "./reads.ts";
import { SourceContent, FieldEvidence } from "./evidence.tsx";
import { identity, promptFor } from "./lib/index.ts";
import type { Occurrence } from "./lib/index.ts";
import { ReadState, PromptBox, Ready, RecordWorkflow, subjectPortfolio } from "./presentation.tsx";

function Occurrences({
  occurrences,
  detail,
}: {
  occurrences: readonly Occurrence[];
  detail?: RecordDetail;
}) {
  const nav = usePageNavigation();
  return (
    <ol className="ae-occurrences">
      {occurrences.map((occurrence, index) => {
        const target = detail?.selectedTargets.find(
          (node) => occurrence.target && identity(node.ref) === identity(occurrence.target),
        );
        return (
          <li key={`${occurrence.ordinal}-${index}`}>
            <strong>{occurrence.link.text || occurrence.link.title || occurrence.authored}</strong>
            <div className="ae-note">{occurrence.authored || occurrence.link.authoredTarget}</div>
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
                Open recorded target
              </RecordChip>
            )}
            {detail && (
              <>
                <p className="ae-note">
                  {occurrence.targetKind === "criterion"
                    ? "Criterion-only selection"
                    : occurrence.targetKind === "story"
                      ? "Whole selected story and its criteria"
                      : "Recorded target"}
                </p>
                {target ? (
                  <SourceContent node={target} />
                ) : (
                  <p role="status">Selected target content unavailable.</p>
                )}
              </>
            )}
          </li>
        );
      })}
    </ol>
  );
}

function Narrative({
  owner,
  name,
  title,
  detail,
}: {
  owner: LiveNode | undefined;
  name: string;
  title: string;
  detail: RecordDetail;
}) {
  const nav = usePageNavigation();
  const state = owner && field(owner, name);
  const refs = state?.sectionNodes ?? [];
  const source =
    refs.length === 1
      ? detail.components.find((node) => identity(node.ref) === identity(refs[0]))
      : undefined;
  const readable = source && !state?.issues?.length;
  return (
    <Section title={title}>
      {readable ? (
        <>
          <SourceContent node={source} />
          <button
            className="ae-link ae-note"
            onClick={() => nav.openNode(source.ref, { view: "builtin:read" })}
          >
            Source {source.ref.notePath}
            {source.ref.fragment ? `#${source.ref.fragment}` : ""}
          </button>
        </>
      ) : (
        <p className="ae-note">
          {!owner
            ? "Source unavailable."
            : !state?.present
              ? "No section recorded."
              : refs.length > 1
                ? "Duplicate sections. Inspect the original source."
                : "Section unreadable. Inspect source issues below."}
        </p>
      )}
    </Section>
  );
}

function OriginalSource({ node }: { node: LiveNode }) {
  const [open, setOpen] = useState(false);
  return (
    <details open={open} onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>{node.title || node.ref.notePath} · original source</summary>
      {open && (
        <>
          <p className="ae-note">{node.ref.notePath}</p>
          <FieldEvidence node={node} />
          <SourceContent node={node} />
        </>
      )}
    </details>
  );
}

export default function Page() {
  const context = useViewContext();
  const query = useDeliveryRecord(context.kind === "node" ? context.ref : null);
  const [selected, setSelected] = useState(false);
  const detail = query.data;
  useEffect(() => setSelected(false), [detail?.node.ref.ref, detail?.node.ref.notePath]);
  const record = detail?.record?.kind === "effort" ? detail.record : null;
  const logRefs = detail
    ? (field(detail.node, "workLog")?.links?.filter((link) => link.resolved && link.ref) ?? [])
    : [];
  const log =
    logRefs.length === 1
      ? detail?.components.find((node) => identity(node.ref) === identity(logRefs[0].ref!))
      : undefined;
  const owner = record?.form === "EffortNote" ? detail?.node : log;
  const approval = detail && field(detail.node, "planApprovedBy");
  const checklist = record?.checklist;
  const same =
    record?.scope.source &&
    detail?.workLogSnapshot &&
    identity(record.scope.source) === identity(detail.workLogSnapshot.ref);
  const nav = usePageNavigation();
  return (
    <main className="ae-page">
      <ReadState query={query} />
      <Ready status={{ status: query.state === "loading" ? "loading" : "ready" }} />
      <RecordPage
        title={
          record?.title ||
          detail?.node.title ||
          (context.kind === "node" ? context.ref.notePath : "Invalid effort route")
        }
        kind="Effort"
        status={{ status: query.state === "loading" ? "loading" : "ready" }}
        partial={
          detail && (!detail.coverage.complete || record?.scope.state !== "known")
            ? "Incomplete execution context. Recorded status remains visible; inspect the source evidence before resuming."
            : undefined
        }
        facts={
          <Facts note={record?.status ?? "Status unavailable"}>
            <Fact>{record?.locator}</Fact>
            <Fact>
              {checklist?.state === "recorded" || checklist?.state === "empty"
                ? `${checklist.checked} of ${checklist.total} checklist items checked`
                : `Checklist ${checklist?.state ?? "unavailable"}`}
            </Fact>
          </Facts>
        }
      >
        {detail && record ? (
          <>
            <RecordWorkflow record={record} />
            <div className="ae-with-aside ae-brief">
              <div>
                <Narrative
                  owner={owner}
                  name="statusSection"
                  title="Where to resume"
                  detail={detail}
                />
                <Narrative
                  owner={owner}
                  name="executionNotes"
                  title="Execution notes"
                  detail={detail}
                />
                <Narrative
                  owner={owner}
                  name="originalIntendedDelivery"
                  title="Original intended delivery"
                  detail={detail}
                />
                <Section
                  title="Frozen selected scope"
                  caption={`Membership ${record.scope.state}. Only the explicit frozen source establishes this selection.`}
                >
                  {record.scope.source && (
                    <p className="ae-note">Selected source {record.scope.source.notePath}</p>
                  )}
                  {record.scope.authored.map((target, i) => (
                    <p className="ae-note" key={i}>
                      Recorded selector {target}
                    </p>
                  ))}
                  {record.scope.reasons.map((reason, i) => (
                    <p role="status" key={i}>
                      {reason}
                    </p>
                  ))}
                  <Occurrences occurrences={record.scope.selections} detail={detail} />
                  {record.scope.state === "known" && !record.scope.selections.length && (
                    <p>No selected story or criterion occurrences recorded.</p>
                  )}
                  <details>
                    <summary>Frozen spec occurrences ({record.scope.specs.length})</summary>
                    <Occurrences occurrences={record.scope.specs} />
                  </details>
                </Section>
              </div>
              <aside aria-label="Closure evidence">
                <Narrative
                  owner={owner}
                  name="closureChecklist"
                  title="Closure checklist"
                  detail={detail}
                />
                <p className="ae-note">
                  Checked items record checklist state. They do not change the effort's status or
                  establish acceptance.
                </p>
                <Narrative
                  owner={owner}
                  name="actualDelivered"
                  title="Actual delivered"
                  detail={detail}
                />
                <Narrative owner={owner} name="deviations" title="Deviations" detail={detail} />
                <Section title="Plan and authorization">
                  {(
                    [
                      ["implementationPlan", "implementation plan"],
                      ["workLog", "work log"],
                      ["materials", "supporting material"],
                    ] as const
                  ).flatMap(([name, label]) =>
                    (field(detail.node, name)?.links ?? []).map((link, index) => (
                      <p key={`${name}-${index}`}>
                        <strong>{label}</strong>
                        <br />
                        {link.resolved && link.ref ? (
                          <button
                            className="ae-link"
                            onClick={() =>
                              link.ref && nav.openNode(link.ref, { view: "builtin:read" })
                            }
                            aria-label={`Open ${label}: ${link.value}`}
                          >
                            {link.title || link.value}
                          </button>
                        ) : (
                          <span>{link.value} · unresolved recorded target</span>
                        )}
                      </p>
                    )),
                  )}
                  {record.form === "EffortNote" && (
                    <Narrative owner={owner} name="plan" title="Recorded plan" detail={detail} />
                  )}
                  {approval?.present ? (
                    <>
                      <p>Recorded approval metadata</p>
                      {approval.values?.map((value, i) => (
                        <p key={i}>{value}</p>
                      ))}
                    </>
                  ) : (
                    <p className="ae-note">
                      No approver recorded. Absence of metadata does not establish absence of
                      authorization.
                    </p>
                  )}
                  <Narrative
                    owner={owner}
                    name="planApproval"
                    title="Plan approval source"
                    detail={detail}
                  />
                </Section>
              </aside>
            </div>
            <Section
              title="Resume with an agent"
              caption="Copy this prompt into your agent. The workflow reads the selected sources and team policy before continuing."
            >
              <label>
                <input
                  type="checkbox"
                  checked={selected}
                  onChange={(event) => setSelected(event.target.checked)}
                />{" "}
                Include this effort in the workflow prompt
              </label>
              <PromptBox
                prompt={{
                  ...promptFor(
                    record.status === "planned"
                      ? "planned"
                      : record.status === "active"
                        ? "active"
                        : "history",
                    subjectPortfolio(record),
                    selected ? [identity(record.ref)] : [],
                  ),
                  ...(query.retained || query.isFetching ? { copyable: false } : {}),
                }}
              />
            </Section>
            <Section
              title="Source evidence"
              caption="Governing metadata, frozen scope, entry mentions, and log snapshots retain their separate authored roles."
            >
              <details>
                <summary>
                  Governing metadata ({detail.governing.occurrences.length} returned links)
                </summary>
                <Occurrences occurrences={detail.governing.occurrences} />
              </details>
              {same ? (
                <p className="ae-note">The work log and frozen scope select the same source.</p>
              ) : (
                <details>
                  <summary>Recorded log scope · separate snapshot</summary>
                  <p className="ae-note">This snapshot does not establish frozen membership.</p>
                  <Occurrences
                    occurrences={
                      detail.workLogSnapshot
                        ? [
                            ...detail.workLogSnapshot.specs.occurrences,
                            ...detail.workLogSnapshot.stories.occurrences,
                          ]
                        : []
                    }
                  />
                </details>
              )}
              {record.form === "EffortWorkspace" && (
                <details>
                  <summary>Entry mentions ({detail.entryMentions.occurrences.length})</summary>
                  <p className="ae-note">
                    Links from the HTML entry do not supply frozen membership.
                  </p>
                  <Occurrences occurrences={detail.entryMentions.occurrences} />
                </details>
              )}
              <button
                className="ae-link"
                onClick={() => nav.openNode(detail.node.ref, { view: "builtin:source" })}
              >
                Open original effort
              </button>
              {[
                detail.node,
                ...detail.components.filter((node) => node.ref.kind.toLowerCase() === "note"),
              ].map((node) => (
                <OriginalSource key={identity(node.ref)} node={node} />
              ))}
            </Section>
          </>
        ) : (
          <p>
            {query.state === "missing-node"
              ? "Effort source unavailable."
              : query.state === "invalid-route"
                ? "Invalid effort route."
                : detail
                  ? "This subject did not resolve to a supported effort."
                  : ""}
          </p>
        )}
      </RecordPage>
    </main>
  );
}
