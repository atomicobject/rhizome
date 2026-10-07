import { sessionStateLabel } from "../staging/stagedState";
import { publicWorkspaceRef } from "../api/client";
import { displayTitle } from "../lib/labels";
import { publicTypeName } from "../lib/typeNames";
import { type ReactNode, useMemo, useState } from "react";
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
import { useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";
import { type TypeLabelFn, useTypeLabel } from "./typeLabels";

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

type Direction = "outgoing" | "incoming" | "both";

// Servers before `direction` existed only marked backlinks.
function itemDirection(item: NoteWorkspaceLink): Direction {
  if (item.direction) return item.direction;

  return item.provenance === "backlink" ? "incoming" : "outgoing";
}

const DIRECTION_GLYPH: Record<Direction, string> = { outgoing: "→", incoming: "←", both: "↔" };

const DIRECTION_LABEL: Record<Direction, string> = {
  outgoing: "Links to",
  incoming: "Links here",
  both: "Linked both ways",
};

type RailRow = {
  key: string;
  target: string;
  label: string;
  title: string;
  typeName: string;
  direction: Direction;
  current?: boolean;
  openable: boolean;
};

type RailBucket = {
  key: string;
  label: string;
  hint?: string;
  direction?: Direction;
  rows: RailRow[];
};

function railRow(item: NoteWorkspaceLink, typeLabel: TypeLabelFn): RailRow {
  const target = relationTarget(item);
  const type = publicTypeName(item.resolvedType || item.structuralNode?.typeName);

  return {
    key: target,
    target,
    label: displayTitle(item.title) || target,
    title: item.targetTitle || item.title || target,
    typeName: type ? typeLabel(type) : "",
    direction: itemDirection(item),
    current: item.current,
    openable: isNoteContent(item),
  };
}

// Typed field relations, one bucket per field and direction ("Impacts →",
// "← Opportunities"). The type moves to the header when every target shares it.
function relationBuckets(items: NoteWorkspaceLink[], typeLabel: TypeLabelFn): RailBucket[] {
  const buckets = new Map<string, RailBucket>();

  for (const item of items) {
    const row = railRow(item, typeLabel);
    const relation = item.relationName || item.provenance || "relation";
    const name = humanizeRelationName(relation);
    // Key by the schema field, not its label: `partOf` and `part_of` stay apart.
    const key = `${row.direction}:${relation}`;
    const bucket = buckets.get(key) ?? { key, label: name, direction: row.direction, rows: [] };

    if (!bucket.rows.some((existing) => existing.target === row.target)) bucket.rows.push(row);
    buckets.set(key, bucket);
  }

  return Array.from(buckets.values()).map((bucket) => {
    const types = new Set(bucket.rows.map((row) => row.typeName));

    if (types.size !== 1) return bucket;

    return {
      ...bucket,
      hint: bucket.rows[0].typeName,
      rows: bucket.rows.map((row) => ({ ...row, typeName: "" })),
    };
  });
}

function folderOf(target: string): string {
  const slash = target.indexOf("/");

  return slash > 0 ? `${target.slice(0, slash)}/` : "Notes";
}

// Body links and backlinks, one row per note with its direction, bucketed by
// type; untyped notes fall back to their top folder ("Log/").
function linkedBuckets(
  items: NoteWorkspaceLink[],
  exclude: ReadonlySet<string>,
  typeLabel: TypeLabelFn,
): RailBucket[] {
  const rows = new Map<string, RailRow>();

  for (const item of items) {
    const row = railRow(item, typeLabel);

    if (exclude.has(row.target)) continue;
    const existing = rows.get(row.target);

    if (!existing) {
      rows.set(row.target, row);
    } else if (existing.direction !== row.direction) {
      existing.direction = "both";
    }
  }

  const buckets = new Map<string, RailBucket & { typed: boolean }>();

  for (const row of rows.values()) {
    const typed = Boolean(row.typeName);
    const label = row.typeName || folderOf(row.target);
    const key = `${typed ? "type" : "folder"}:${label}`;
    const bucket = buckets.get(key) ?? { key, label, typed, rows: [] };
    bucket.rows.push({ ...row, typeName: "" });
    buckets.set(key, bucket);
  }

  return Array.from(buckets.values()).sort(
    (a, b) =>
      Number(b.typed) - Number(a.typed) ||
      b.rows.length - a.rows.length ||
      a.label.localeCompare(b.label),
  );
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

type OpenFn = (path: string, target?: "current" | "stack" | "beside") => void;

function RailRowItem({
  row,
  showDirection,
  from,
  onOpen,
}: {
  row: RailRow;
  showDirection: boolean;
  from: string;
  onOpen: OpenFn;
}) {
  return (
    <li className="rail-row">
      {showDirection && (
        <span
          className={`rail-row__dir rail-row__dir--${row.direction}`}
          role="img"
          aria-label={DIRECTION_LABEL[row.direction]}
          title={DIRECTION_LABEL[row.direction]}
        >
          {DIRECTION_GLYPH[row.direction]}
        </span>
      )}
      {row.openable ? (
        <SidebarPreviewButton
          target={row.target}
          label={row.label}
          title={row.title}
          current={row.current}
          from={from}
          onOpen={onOpen}
        />
      ) : (
        <span className="rail-row__label" title={row.title}>
          {row.label}
        </span>
      )}
      {row.typeName && <span className="rail-row__type">{row.typeName}</span>}
    </li>
  );
}

const BUCKET_LIMIT = 5;

function RailBucketList({
  bucket,
  showDirection,
  from,
  onOpen,
}: {
  bucket: RailBucket;
  showDirection: boolean;
  from: string;
  onOpen: OpenFn;
}) {
  const [expanded, setExpanded] = useState(false);
  // Show everything when only one row would hide behind the toggle.
  const limit = bucket.rows.length <= BUCKET_LIMIT + 1 ? bucket.rows.length : BUCKET_LIMIT;
  const rows = expanded ? bucket.rows : bucket.rows.slice(0, limit);
  const hidden = bucket.rows.length - rows.length;

  return (
    <div className="rail-bucket">
      <div className="rail-bucket__head">
        {bucket.direction && bucket.direction !== "outgoing" && (
          <span className="rail-bucket__dir" aria-hidden="true">
            {DIRECTION_GLYPH[bucket.direction]}
          </span>
        )}
        <span className="rail-bucket__label">{bucket.label}</span>
        {bucket.direction === "outgoing" && (
          <span className="rail-bucket__dir" aria-hidden="true">
            {DIRECTION_GLYPH.outgoing}
          </span>
        )}
        {bucket.direction && <span className="sr-only">{DIRECTION_LABEL[bucket.direction]}</span>}
        {bucket.hint && <span className="rail-bucket__hint">{bucket.hint}</span>}
        <span className="rail-bucket__count">{bucket.rows.length}</span>
      </div>
      <ul>
        {rows.map((row) => (
          <RailRowItem
            key={row.key}
            row={row}
            showDirection={showDirection}
            from={from}
            onOpen={onOpen}
          />
        ))}
      </ul>
      {bucket.rows.length > limit && (
        <button
          type="button"
          className="rail-bucket__more"
          aria-expanded={expanded}
          onClick={() => setExpanded((value) => !value)}
        >
          {expanded ? "Show fewer" : `${hidden} more`}
        </button>
      )}
    </div>
  );
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
        {count !== undefined && <span className="ontology-sidebar-context__count">{count}</span>}
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
}: {
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
      ),
    };
  }, [relationGroups, workspace, path, typeLabel]);

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
    <section className="ontology-sidebar-info">
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
          {navigationGroups.map((group) => (
            <SidebarContextGroup key={group.key} title={group.label}>
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
            </SidebarContextGroup>
          ))}
          <SidebarContextGroup title="Relations" count={rowCount(relations)}>
            {relations.length > 0 &&
              relations.map((bucket) => (
                <RailBucketList
                  key={bucket.key}
                  bucket={bucket}
                  showDirection={false}
                  from={path}
                  onOpen={onOpen}
                />
              ))}
          </SidebarContextGroup>
          <SidebarContextGroup title="Linked notes" count={rowCount(linked)}>
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
