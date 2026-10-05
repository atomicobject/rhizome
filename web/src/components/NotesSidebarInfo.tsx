import { sessionStateLabel } from "../staging/stagedState";
import { publicWorkspaceRef } from "../api/client";
import { displayTitle } from "../lib/labels";
import { publicTypeName } from "../lib/typeNames";
import { type ReactNode, useMemo } from "react";
import type {
  NodeWorkspace,
  NoteWorkspaceGroup,
  NoteWorkspaceLink,
  WorkspaceContentNode,
  WorkspaceFieldNode,
} from "../api/types";
import { selectWorkspaceRelationGroups } from "./workspaceGraph";
import { ValidationIssueBadge } from "./validation/ValidationIssueBadge";
import type { ValidationHealth } from "../api/types";
import { useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";
import { useTypeLabel } from "./typeLabels";

// Only genuine code evidence belongs under the Code rail. Section and embedded
// targets (headings, action items) are note content and stay with Related notes.
const NOTE_CONTENT_KINDS = new Set(["note", "section", "embedded"]);

function isCodeContextGroup(group: NoteWorkspaceGroup): boolean {
  return group.key === "code" || group.items?.some((item) => !isNoteContent(item)) || false;
}

function isNoteContent(item: NoteWorkspaceLink): boolean {
  return NOTE_CONTENT_KINDS.has(item.kind);
}

// Anchored items address a node inside a note; the shell opens `path#anchor`.
function relationTarget(item: NoteWorkspaceLink): string {
  return item.anchor && !item.path.includes("#") ? `${item.path}#${item.anchor}` : item.path;
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

// Link kinds carry an index qualifier ("note_link:wikilink:basic"); readers
// only need the kind.
function humanizeRelationName(name: string): string {
  const spaced = name
    .split(":", 1)[0]
    .trim()
    .replace(/[_-]+/g, " ")
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .toLowerCase();

  return spaced ? spaced[0].toUpperCase() + spaced.slice(1) : name;
}

// Server groups can share a reader-facing label (two link groups); show one
// bucket per label and each target once.
function mergeBucketsByLabel(groups: NoteWorkspaceGroup[]): NoteWorkspaceGroup[] {
  const byLabel = new Map<string, NoteWorkspaceGroup & { items: NoteWorkspaceLink[] }>();

  for (const group of groups) {
    const label = compactRelationLabel(group);
    const bucket = byLabel.get(label);

    if (!bucket) {
      byLabel.set(label, { ...group, label, items: [...(group.items || [])] });
      continue;
    }

    for (const item of group.items || []) {
      const target = relationTarget(item);

      if (!bucket.items.some((existing) => relationTarget(existing) === target)) {
        bucket.items.push(item);
      }
    }
  }

  return Array.from(byLabel.values());
}

function relationGroupWithItems(
  group: NoteWorkspaceGroup,
  items: NoteWorkspaceLink[],
): NoteWorkspaceGroup | null {
  if (items.length === 0) return null;

  return { ...group, items };
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

function SidebarPreviewButton({
  target,
  label,
  title,
  current,
  from,
  onOpen,
}: {
  target: string;
  label: string;
  title?: string;
  current?: boolean;
  from: string;
  onOpen: (path: string, target?: "current" | "stack" | "beside") => void;
}) {
  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target,
    from,
    placement: "left",
    placementBoundary: (trigger) =>
      trigger.closest(".ontology-sidebar-info")?.getBoundingClientRect() ?? null,
    open: (path, mode) => onOpen(path, mode),
  });

  return (
    <>
      <button
        {...previewTrigger.triggerProps}
        type="button"
        title={title || target}
        aria-current={current ? "page" : undefined}
        onClick={(event) => {
          previewTrigger.close();
          onOpen(target, event.metaKey || event.ctrlKey ? "beside" : "stack");
        }}
      >
        {label}
      </button>
      {previewTrigger.preview}
    </>
  );
}

function SidebarContextLink({
  item,
  group,
  from,
  onOpen,
}: {
  item: NoteWorkspaceLink;
  group: NoteWorkspaceGroup;
  from: string;
  onOpen: (path: string, target?: "current" | "stack" | "beside") => void;
}) {
  const relation = item.relationName
    ? humanizeRelationName(item.relationName)
    : compactRelationLabel(group);

  const provenance = item.provenance ? humanizeRelationName(item.provenance) : "";
  const typeLabel = useTypeLabel();

  const meta = [
    item.current ? "Current" : null,
    publicTypeName(item.resolvedType) && typeLabel(item.resolvedType),
    relation,
    provenance && provenance !== relation ? provenance.toLowerCase() : null,
  ].filter(Boolean);

  const target = relationTarget(item);

  return (
    <li className="ontology-sidebar-context__item">
      {isNoteContent(item) ? (
        <SidebarPreviewButton
          target={target}
          label={displayTitle(item.title) || target}
          title={item.targetTitle || item.title || target}
          current={item.current}
          from={from}
          onOpen={onOpen}
        />
      ) : (
        <span>{displayTitle(item.title) || target}</span>
      )}
      {meta.length > 0 && <div className="ontology-sidebar-context__meta">{meta.join(" · ")}</div>}
      <div className="ontology-sidebar-context__path" title={target}>
        {target}
      </div>
    </li>
  );
}

// Empty groups stay hidden; the panel says once when nothing is indexed.
function SidebarContextGroup({ title, children }: { title: string; children?: ReactNode }) {
  if (!children) return null;

  return (
    <section className="ontology-sidebar-context__group">
      <h3>{title}</h3>
      {children}
    </section>
  );
}

export function NotesSidebarInfo({
  workspace,
  onOpen,
  validationIssueCount,
  validationHealth = "never_checked",
}: {
  workspace: NodeWorkspace | null;
  onOpen: (path: string, target?: "current" | "stack" | "beside") => void;
  validationIssueCount?: number;
  validationHealth?: ValidationHealth;
}) {
  const relationGroups = useMemo(
    () => (workspace ? selectWorkspaceRelationGroups(workspace, workspace.focusedNodeId) : []),
    [workspace],
  );

  const navigationGroups = relationGroups.filter((group) => group.navigation);

  const nonCodeGroups = relationGroups.filter(
    (group) => !group.navigation && !isCodeContextGroup(group),
  );

  const codeGroups = relationGroups.filter(isCodeContextGroup);

  const relatedGroups = nonCodeGroups.flatMap((group) => {
    const related = relationGroupWithItems(
      group,
      (group.items || []).filter((item) => item.provenance !== "backlink"),
    );

    return related ? [related] : [];
  });

  const incomingGroups = nonCodeGroups.flatMap((group) => {
    const incoming = relationGroupWithItems(
      group,
      (group.items || []).filter((item) => item.provenance === "backlink"),
    );

    return incoming ? [incoming] : [];
  });

  const nearbyNodes = useMemo(
    () => (workspace ? nearbyStructuralNodes(workspace) : []),
    [workspace],
  );

  const issues = workspace?.content?.assessment?.issues || [];

  const focusedFields = useMemo(
    () =>
      (workspace?.nodes || []).filter(
        (node): node is WorkspaceFieldNode =>
          node.kind === "field" &&
          (!workspace?.focusedNodeId || node.parentId === workspace.focusedNodeId),
      ),
    [workspace],
  );

  const sectionFields = focusedFields.filter((node) => (node.field.sectionRefs?.length ?? 0) > 0);
  const typeLabel = useTypeLabel();

  const path =
    workspace?.node?.notePath || workspace?.content?.path || workspace?.requestedRef || "";

  if (!workspace) {
    return (
      <section className="ontology-sidebar-info ontology-sidebar-info--empty">
        <div className="ontology-sidebar-info__empty">
          Open or focus a note to inspect its context.
        </div>
      </section>
    );
  }

  const title = displayTitle(workspace.content?.title || workspace.node?.title) || "Untitled";

  const resolvedType = publicTypeName(
    workspace.content?.resolvedType || workspace.node?.resolvedType,
  );

  const typeName = resolvedType && typeLabel(resolvedType);

  const issueCount = validationIssueCount;

  return (
    <section className="ontology-sidebar-info">
      <div className="ontology-sidebar-info__meta">
        <header className="ontology-sidebar-context__identity">
          <div className={`ontology-sidebar-info__type type-chip${typeName ? "" : " is-untyped"}`}>
            {typeName || "Untyped"}
          </div>
          <h2>{title}</h2>
          <div className="ontology-sidebar-info__path" title={path}>
            {path}
          </div>
          <div className="ontology-sidebar-context__chips">
            <ValidationIssueBadge
              count={issueCount}
              health={validationHealth}
              showUnit
              label={`Validation issues in ${title}`}
            />
            {workspace.status?.dirty && <span>Staged</span>}
            {sessionStateLabel(workspace.status?.session?.state) && (
              <span>{sessionStateLabel(workspace.status?.session?.state)}</span>
            )}
          </div>
        </header>

        <section className="ontology-sidebar-context" aria-label="Focused note context">
          {navigationGroups.map((group) => (
            <SidebarContextGroup key={group.key} title={group.label}>
              <div className="ontology-sidebar-context__stack">
                {group.ownerTitle && (
                  <div className="ontology-sidebar-context__owner">{group.ownerTitle}</div>
                )}
                <ul>
                  {(group.items || []).map((item, index) => (
                    <SidebarContextLink
                      key={relationItemKey(group, item, index)}
                      item={item}
                      group={group}
                      from={path}
                      onOpen={onOpen}
                    />
                  ))}
                </ul>
              </div>
            </SidebarContextGroup>
          ))}
          <SidebarContextGroup title="Sections">
            {sectionFields.length > 0 && (
              <ul>
                {sectionFields.flatMap((node) =>
                  (node.field.sectionRefs || []).map((ref, index) => {
                    const target = publicWorkspaceRef(ref);

                    return (
                      <li
                        key={`${node.id}-${target}-${index}`}
                        className="ontology-sidebar-context__item"
                      >
                        <SidebarPreviewButton
                          target={target}
                          label={node.field.name}
                          from={path}
                          onOpen={onOpen}
                        />
                        <div className="ontology-sidebar-context__path" title={target}>
                          {ref.fragment || ref.nodeId || ref.notePath}
                        </div>
                      </li>
                    );
                  }),
                )}
              </ul>
            )}
          </SidebarContextGroup>
          <SidebarContextGroup title="Related notes">
            {relatedGroups.length > 0 && (
              <ContextGroupList groups={relatedGroups} limit={6} from={path} onOpen={onOpen} />
            )}
          </SidebarContextGroup>
          <SidebarContextGroup title="Incoming references">
            {incomingGroups.length > 0 && (
              <ContextGroupList groups={incomingGroups} limit={6} from={path} onOpen={onOpen} />
            )}
          </SidebarContextGroup>
          <SidebarContextGroup title="Code">
            {codeGroups.length > 0 && (
              <ContextGroupList groups={codeGroups} limit={8} from={path} onOpen={onOpen} />
            )}
          </SidebarContextGroup>
          {issues.length > 0 && (
            <SidebarContextGroup title="Problems">
              {issues.length > 0 && (
                <ul>
                  {issues.slice(0, 5).map((issue) => (
                    <li
                      key={`${issue.code || "issue"}-${issue.message || ""}`}
                      className="ontology-sidebar-context__item"
                    >
                      <span>{issue.message || issue.code || "Unknown issue"}</span>
                      {issue.code && (
                        <div className="ontology-sidebar-context__meta">{issue.code}</div>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </SidebarContextGroup>
          )}
          <SidebarContextGroup title="Nearby nodes">
            {nearbyNodes.length > 0 && (
              <ul>
                {nearbyNodes.map((node) => (
                  <li key={node.id} className="ontology-sidebar-context__item">
                    <SidebarPreviewButton
                      target={publicWorkspaceRef(node.ref)}
                      label={node.data.title || node.notePath}
                      from={path}
                      onOpen={onOpen}
                    />
                    <div className="ontology-sidebar-context__meta">
                      {publicTypeName(node.data.binding?.typeName || node.data.resolvedType) ||
                        node.kind}
                    </div>
                  </li>
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

function ContextGroupList({
  groups,
  limit,
  from,
  onOpen,
}: {
  groups: NoteWorkspaceGroup[];
  limit: number;
  from: string;
  onOpen: (path: string, target?: "current" | "stack" | "beside") => void;
}) {
  return (
    <div className="ontology-sidebar-context__stack">
      {mergeBucketsByLabel(groups).map((group) => (
        <div key={group.key} className="ontology-sidebar-context__bucket">
          <div className="ontology-sidebar-context__bucket-title">
            {compactRelationLabel(group)} · {(group.items || []).length}
          </div>
          <ul>
            {(group.items || []).slice(0, limit).map((item, index) => (
              <SidebarContextLink
                key={relationItemKey(group, item, index)}
                item={item}
                group={group}
                from={from}
                onOpen={onOpen}
              />
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}
