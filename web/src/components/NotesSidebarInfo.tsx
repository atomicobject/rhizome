import {
  railRow,
  relationBuckets,
  linkedBuckets,
  RELATION_DIRECTIONS,
  RELATION_DIRECTION_LABEL,
  DIRECTION_GLYPH,
  isNoteContent,
  relationTarget,
  humanizeRelationName,
} from "./body/noteInfoBuckets";
import { sessionStateLabel } from "../staging/stagedState";
import { publicWorkspaceRef } from "../api/client";
import { displayTitle } from "../lib/labels";
import { publicTypeName } from "../lib/typeNames";
import { DisclosureChevron } from "./DisclosureChevron";
import {
  DirectionLegend,
  RailBucketList,
  RailRowItem,
  type OpenFn,
  type RailBucket,
} from "./body/NoteInfoRows";
import { type ReactNode, useMemo } from "react";
import type {
  NodeWorkspace,
  NoteWorkspaceGroup,
  NoteWorkspaceLink,
  WorkspaceContentNode,
} from "../api/types";
import { isContentNode } from "../api/types";
import { selectWorkspaceRelationGroups } from "./workspaceGraph";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";
import type { ValidationHealth } from "../api/types";
import { useTypeLabel } from "./typeLabels";

// Only genuine code evidence belongs under the Code rail. Section and embedded
// targets (headings, action items) are note content and stay with Related notes.
function isCodeContextGroup(group: NoteWorkspaceGroup): boolean {
  return group.key === "code" || group.items?.some((item) => !isNoteContent(item)) || false;
}

function relationItemKey(
  group: NoteWorkspaceGroup,
  item: NoteWorkspaceLink,
  index: number,
): string {
  return `${group.key}-${relationTarget(item)}-${item.relationName || ""}-${index}`;
}

function compactRelationLabel(group: NoteWorkspaceGroup): string {
  const relationSet = new Set<string>();

  for (const item of group.items || []) {
    const relation = item.relationName || item.provenance || "";

    if (relation) relationSet.add(relation);
  }

  const relations = Array.from(relationSet);

  if (relations.length === 1) return humanizeRelationName(relations[0]);

  return group.label;
}

function nearbyStructuralNodes(workspace: NodeWorkspace): WorkspaceContentNode[] {
  const focusedID = workspace.focusedNodeId;

  if (!focusedID) return [];

  const nodes = (workspace.nodes || []).filter(
    (node): node is WorkspaceContentNode =>
      node.kind === "note" || node.kind === "section" || node.kind === "embedded",
  );

  const focused = nodes.find((node) => node.id === focusedID);

  if (!focused || focused.kind === "note") return [];
  const output: WorkspaceContentNode[] = [];
  const parent = focused.parentId ? nodes.find((node) => node.id === focused.parentId) : null;

  if (parent) output.push(parent);
  output.push(
    ...nodes
      .filter(
        (node) =>
          node.id !== focused.id && node.id !== parent?.id && node.parentId === focused.parentId,
      )
      .slice(0, 5),
  );

  return output;
}

// "In this effort" names its members as "Effort materials".
function materialsLabel(label: string): string {
  const owner = /^In this (.+)$/.exec(label)?.[1];

  return owner ? `${owner[0].toUpperCase()}${owner.slice(1)} materials` : label;
}

// Empty groups stay hidden; the panel says once when nothing is indexed.
function SidebarContextGroup({
  title,
  count,
  children,
}: {
  title: string;
  count?: number;
  children?: ReactNode;
}) {
  if (!children) return null;

  return (
    <section className="ontology-sidebar-context__group">
      <h3>
        {title}
        {count !== undefined && (
          <span className="ontology-sidebar-context__count num">{count}</span>
        )}
      </h3>
      {children}
    </section>
  );
}

function rowCount(buckets: RailBucket[]): number {
  return buckets.reduce((total, bucket) => total + bucket.rows.length, 0);
}

export function NotesSidebarInfo({
  workspace,
  onOpen,
  validationIssueCount,
  validationHealth = "never_checked",
  noteReader = false,
}: {
  noteReader?: boolean;
  workspace: NodeWorkspace | null;
  onOpen: OpenFn;
  validationIssueCount?: number;
  validationHealth?: ValidationHealth;
}) {
  const typeLabel = useTypeLabel();

  const path =
    workspace?.node?.notePath || workspace?.content?.path || workspace?.requestedRef || "";

  const relationGroups = useMemo(
    () => (workspace ? selectWorkspaceRelationGroups(workspace, workspace.focusedNodeId) : []),
    [workspace],
  );

  const navigationGroups = relationGroups.filter((group) => group.navigation);
  const codeGroups = relationGroups.filter(isCodeContextGroup);

  const { relations, linked } = useMemo(() => {
    const noteItems = relationGroups
      .filter((group) => !group.navigation && !isCodeContextGroup(group))
      .flatMap((group) => group.items || []);

    // Section-valued fields that point into other notes are relations too;
    // this note's own sections are already on the page and in Outline.
    const sectionItems: NoteWorkspaceLink[] = (workspace?.nodes || []).flatMap((node) =>
      node.kind === "field" &&
      (!workspace?.focusedNodeId || node.parentId === workspace.focusedNodeId)
        ? (node.field.sectionRefs || [])
            .filter((ref) => ref.notePath !== path)
            .map((ref) => ({
              path: publicWorkspaceRef(ref),
              title: ref.fragment || ref.nodeId || ref.notePath,
              kind: "section",
              relationName: node.field.name,
              structural: true,
            }))
        : [],
    );

    const relationBucketsList = relationBuckets(
      [...noteItems.filter((item) => item.structural), ...sectionItems],
      typeLabel,
      noteReader,
    );

    const relationTargets = new Set(
      relationBucketsList.flatMap((bucket) => bucket.rows.map((row) => row.target)),
    );

    return {
      relations: relationBucketsList,
      linked: linkedBuckets(
        noteItems.filter((item) => !item.structural),
        relationTargets,
        typeLabel,
        noteReader,
      ),
    };
  }, [relationGroups, workspace, path, typeLabel, noteReader]);

  const nearbyNodes = useMemo(
    () => (workspace ? nearbyStructuralNodes(workspace) : []),
    [workspace],
  );

  if (!workspace) {
    return (
      <section className="ontology-sidebar-info ontology-sidebar-info--empty">
        <div className="ontology-sidebar-info__empty">
          Open or focus a note to inspect its context.
        </div>
      </section>
    );
  }

  const issues = workspace.content?.assessment?.issues || [];
  const title = displayTitle(workspace.content?.title || workspace.node?.title) || "Untitled";
  const sessionLabel = sessionStateLabel(workspace.status?.session?.state);

  const focusedSection = (workspace.nodes || []).find(
    (node) => node.id === workspace.focusedNodeId && node.kind !== "note" && isContentNode(node),
  );

  return (
    <section className={`ontology-sidebar-info${noteReader ? " note-info" : ""}`}>
      <div className="ontology-sidebar-info__meta">
        <header className="ontology-sidebar-context__identity">
          {focusedSection && isContentNode(focusedSection) && (
            <h2 className="ontology-sidebar-context__focus" title={path}>
              {focusedSection.data.title || focusedSection.kind}
            </h2>
          )}
          <div className="ontology-sidebar-context__chips">
            <ValidationIssueBadge
              count={validationIssueCount}
              health={validationHealth}
              showUnit
              label={`Validation issues in ${title}`}
            />
            {workspace.status?.dirty && <span>Staged</span>}
            {sessionLabel && <span>{sessionLabel}</span>}
          </div>
        </header>

        <section className="ontology-sidebar-context" aria-label="Focused note context">
          {/* A workspace's own navigation often repeats the page's, so it waits
              behind a disclosure. An effort's materials (overview, plan, log)
              are the rail's most useful group on a Markdown note, so they start
              open there; an HTML page links them itself, so they stay closed. */}
          {navigationGroups.map((group) => (
            <details
              key={group.key}
              className="ontology-sidebar-context__group rail-materials"
              open={group.label === "In this effort" && workspace.content?.format !== "html"}
            >
              <summary>
                <h3>
                  {materialsLabel(group.label)}
                  {noteReader && (
                    <span className="disclosure-toggle" aria-hidden="true">
                      <DisclosureChevron />
                    </span>
                  )}
                  <span className="ontology-sidebar-context__count num">
                    {group.items?.length ?? 0}
                    {!noteReader && (
                      <span className="rail-materials__unit">
                        {group.items?.length === 1 ? " material" : " materials"}
                      </span>
                    )}
                  </span>
                  {!noteReader && <DisclosureChevron />}
                </h3>
              </summary>
              {group.ownerTitle && (
                <div className="ontology-sidebar-context__owner">{group.ownerTitle}</div>
              )}
              <ul>
                {(group.items || []).map((item, index) => (
                  <RailRowItem
                    key={relationItemKey(group, item, index)}
                    row={railRow(item, typeLabel)}
                    showDirection={false}
                    from={path}
                    onOpen={onOpen}
                  />
                ))}
              </ul>
            </details>
          ))}
          <SidebarContextGroup title="Relations" count={rowCount(relations)}>
            {noteReader
              ? relations.map((bucket) => (
                  <RailBucketList
                    key={bucket.key}
                    bucket={bucket}
                    showDirection
                    from={path}
                    onOpen={onOpen}
                  />
                ))
              : relations.length > 0 &&
                RELATION_DIRECTIONS.map((direction) => {
                  const buckets = relations.filter((bucket) => bucket.direction === direction);

                  if (buckets.length === 0) return null;

                  return (
                    <section key={direction} aria-label={RELATION_DIRECTION_LABEL[direction]}>
                      <h4 className="rail-direction">
                        <span className="rail-direction__dir" aria-hidden="true">
                          {DIRECTION_GLYPH[direction]}
                        </span>
                        {RELATION_DIRECTION_LABEL[direction]}
                      </h4>
                      {buckets.map((bucket) => (
                        <RailBucketList
                          key={bucket.key}
                          bucket={bucket}
                          showDirection={false}
                          from={path}
                          onOpen={onOpen}
                        />
                      ))}
                    </section>
                  );
                })}
          </SidebarContextGroup>
          <SidebarContextGroup title="Linked notes" count={rowCount(linked)}>
            {linked.length > 0 && <DirectionLegend buckets={linked} />}
            {linked.length > 0 &&
              linked.map((bucket) => (
                <RailBucketList
                  key={bucket.key}
                  bucket={bucket}
                  showDirection
                  from={path}
                  onOpen={onOpen}
                />
              ))}
          </SidebarContextGroup>
          <SidebarContextGroup title="Code">
            {codeGroups.length > 0 &&
              codeGroups.map((group) => (
                <RailBucketList
                  key={group.key}
                  bucket={{
                    key: group.key,
                    label: compactRelationLabel(group),
                    rows: (group.items || []).map((item) => railRow(item, typeLabel)),
                  }}
                  showDirection={false}
                  from={path}
                  onOpen={onOpen}
                />
              ))}
          </SidebarContextGroup>
          {issues.length > 0 && (
            <SidebarContextGroup title="Problems" count={issues.length}>
              <ul>
                {issues.slice(0, 5).map((issue) => (
                  <li
                    key={`${issue.code || "issue"}-${issue.message || ""}`}
                    className="rail-row rail-row--problem"
                    title={issue.code}
                  >
                    <span className="rail-row__label">
                      {issue.message || issue.code || "Unknown issue"}
                    </span>
                  </li>
                ))}
              </ul>
            </SidebarContextGroup>
          )}
          <SidebarContextGroup title="Nearby nodes">
            {nearbyNodes.length > 0 && (
              <ul>
                {nearbyNodes.map((node) => (
                  <RailRowItem
                    key={node.id}
                    row={{
                      key: node.id,
                      target: publicWorkspaceRef(node.ref),
                      label: node.data.title || node.notePath,
                      title: node.data.title || node.notePath,
                      typeName:
                        publicTypeName(node.data.binding?.typeName || node.data.resolvedType) ||
                        node.kind,
                      direction: "outgoing",
                      openable: true,
                    }}
                    showDirection={false}
                    from={path}
                    onOpen={onOpen}
                  />
                ))}
              </ul>
            )}
          </SidebarContextGroup>
        </section>
        {relationGroups.length === 0 && issues.length === 0 && (
          <p className="ontology-sidebar-context__empty ontology-sidebar-context__empty--panel">
            No relationship context has been indexed for this note yet.
          </p>
        )}
      </div>
    </section>
  );
}
