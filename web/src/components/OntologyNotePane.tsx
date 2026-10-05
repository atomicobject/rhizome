import { publicTypeName } from "../lib/typeNames";
import type { MutableRefObject, ReactNode } from "react";
import { createElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Components as MarkdownComponents } from "react-markdown";
import type {
  NodeLocator,
  NodeStatus,
  NodeWorkspace,
  OntologyEditOp,
  OntologyEditSessionResponse,
  RenderedFile,
  RenderedSection,
  StructuralNode,
  ValidationHealth,
  WorkspaceFieldNode,
} from "../api/types";
import {
  buildNoteWebHref,
  convertWikilinks,
  resolveKnownRenderedLinkTarget,
  resolveRenderedLinkTarget,
} from "../lib/content";
import { displayTitle } from "../lib/labels";
import { BodyWalker } from "./body/BodyWalker";
import { EmptyNarrativeEditor, SourceEditor } from "./editing/SourceEditor";
import type { BodyRenderContext } from "./body/registry";
import type { NoteTabContext, NoteViewMode } from "./noteTabContext";
import { canonicalNodeRefKey } from "./nodeRef";
import {
  changedCollections,
  changedFields,
  isTouched,
  sessionStateLabel,
} from "../staging/stagedState";
import { HTMLNoteViewer } from "./HTMLNoteViewer";
import { Markdown } from "./markdown/Markdown";
import { NoteLinkPreview } from "./notePreview/NoteLinkPreview";
import { MarkdownEditor } from "./markdownEditor/MarkdownEditor";
import type { MarkdownEditorHandle } from "./markdownEditor/types";
import { useRhizomeMarkdownLinks } from "./markdownEditor/useRhizomeMarkdownLinks";
import { HTMLRootMetadataPanel } from "./HTMLRootMetadataPanel";
import { OntologyIdentityStrip } from "./OntologyIdentityStrip";
import { OntologyPropertyPanel } from "./OntologyPropertyPanel";
import { pickIdentityFields } from "./ontologyFieldPicking";
import { TypedSectionCard } from "./TypedSectionCard";
import { selectWorkspaceRenderedSections, selectWorkspaceStructuralView } from "./workspaceGraph";
import {
  issueTargetToTextRange,
  type IssueTarget,
  utf8ByteRangeToLineRange,
} from "./validation/issueNavigation";
import { useTypeLabel } from "./typeLabels";

type Props = {
  loading?: boolean;
  error?: string | null;
  onRetry?: () => void;
  rendered?: RenderedFile | null;
  workspace: NodeWorkspace | null;
  initialAnchor?: string | null;
  documentQuery?: string | null;
  applicationHref?: string;
  onOpen: (path: string, target?: "current" | "stack" | "beside") => void;
  /** Open a structural node inside the current note tab. */
  onOpenNode?: (node: StructuralNode) => void;
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | undefined;
  /** Vault owner for raw editor draft persistence. */
  vaultKey?: string | null;
  onContextChange?: (context: NoteTabContext | null) => void;
  viewMode?: NoteViewMode | "custom";
  presentationControls?: ReactNode;
  renderPresentation?: (body: ReactNode) => ReactNode;
  onViewModeChange?: (mode: NoteViewMode) => void;
  contextCollapsed?: boolean;
  onToggleContext?: () => void;
  breadcrumbs?: ReactNode;
  editing?: boolean;
  editSession?: OntologyEditSessionResponse | null;
  issueTarget?: IssueTarget | null;
  issueFallback?: boolean;
  validationIssueCount?: number;
  validationHealth?: ValidationHealth;
  onOpenIssues?: () => void;
};

type OpenTarget = "current" | "stack" | "beside";

function scrollBehavior(): ScrollBehavior {
  return window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth";
}

function openFromClick(
  event: React.MouseEvent<HTMLElement>,
  path: string,
  onOpen: (path: string, target?: OpenTarget) => void,
  fallback: Exclude<OpenTarget, "beside">,
): void {
  if (event.button !== 0 || event.shiftKey || event.altKey) return;
  event.preventDefault();
  onOpen(path, event.metaKey || event.ctrlKey ? "beside" : fallback);
}

function focusedNodeLocator(workspace: NodeWorkspace | null): NodeLocator | null {
  if (workspace?.nodeLocator) return workspace.nodeLocator;

  if (workspace?.node?.nodeLocator) return workspace.node.nodeLocator;

  if (!workspace?.linkTarget) return null;
  const ref = workspace.linkTarget.ref || workspace.node.ref;

  return {
    ref,
    kind: ref.kind,
    sourceLocator: workspace.node?.ref
      ? canonicalNodeRefKey(workspace.node.ref)
      : workspace.requestedRef || "",
    status: workspace.linkTarget.requiresFix ? "requires_fix" : "linkable",
    linkTarget: workspace.linkTarget,
  };
}

function locatorDiagnostic(locator: NodeLocator | null): string {
  if (!locator) return "No node locator available.";
  const diagnostic = locator.diagnostics?.find((item) => item.message);

  if (diagnostic?.message) return diagnostic.message;

  switch (locator.status) {
    case "requires_fix":
      return "This node needs a block ID before its link is durable.";
    case "unsupported":
      return "This node type does not have a durable link target yet.";
    case "unresolved":
      return "This node could not be resolved to a link target.";
    default:
      return "No link target available.";
  }
}

function flattenSections(
  sections: RenderedSection[],
  trail: number[] = [],
): Array<{ section: RenderedSection; key: string }> {
  return sections.flatMap((section, index) => {
    const key = [...trail, index].join("-");

    return [{ section, key }, ...flattenSections(section.children || [], [...trail, index])];
  });
}

/**
 * True if any section in the tree carries an ontology-assigned typeName.
 * When false, the body falls back to a single Markdown render so
 * plain notes don't pay the walker's heading-reconstruction cost and don't
 * lose any pre-heading preface text.
 */
function anySectionTyped(sections: RenderedSection[]): boolean {
  for (const section of sections) {
    if (section.typeName) return true;

    if (anySectionTyped(section.children || [])) return true;
  }

  return false;
}

function nodeRefLikeForWorkspace(workspace: NodeWorkspace | null, rendered: RenderedFile | null) {
  if (workspace?.node?.ref) return workspace.node.ref;

  if (!rendered?.path) return null;
  const hashIndex = rendered.path.indexOf("#");
  const notePath = hashIndex === -1 ? rendered.path : rendered.path.slice(0, hashIndex);
  const fragment = hashIndex === -1 ? undefined : rendered.path.slice(hashIndex + 1) || undefined;
  const kind: "NOTE" | "SECTION" = fragment ? "SECTION" : "NOTE";

  return {
    notePath,
    fragment,
    kind,
  };
}

function notePathFromLocator(path?: string | null): string | null {
  if (!path) return null;
  const hashIndex = path.indexOf("#");

  return hashIndex === -1 ? path : path.slice(0, hashIndex);
}

function mergeStatus(base: NodeStatus, patch: Partial<NodeStatus>): NodeStatus {
  return {
    ...base,
    ...patch,
    validation: {
      ...base.validation,
      ...patch.validation,
    },
    freshness: {
      ...base.freshness,
      ...patch.freshness,
    },
    session: {
      ...base.session,
      ...patch.session,
    },
  };
}

function applyDirtyToFieldNodes(
  fields: WorkspaceFieldNode[],
  changedFieldNames: Set<string>,
): WorkspaceFieldNode[] {
  return fields.map((node) => {
    return {
      ...node,
      status: mergeStatus(node.status, {
        dirty: changedFieldNames.has(node.field.name),
      }),
    };
  });
}

function normalizeHeadingText(raw: string): string {
  let title = raw.trim();

  if (title === "") return "";
  let end = title.length;

  while (end > 0 && title[end - 1] === "#") {
    end--;
  }

  if (end < title.length) {
    const trimmed = title.slice(0, end).replace(/[ \t]+$/, "");

    if (trimmed === "" || trimmed.length !== title.slice(0, end).length) {
      title = trimmed;
    }
  }

  return title.trim();
}

// extractPrefaceMarkdown mirrors the backend section parser closely enough to
// keep any text before the first parsed heading visible when the typed-section
// walker takes over rendering for mixed notes.
function extractPrefaceMarkdown(markdown: string): string {
  if (!markdown) return "";
  const lines = markdown.split("\n");
  let offset = 0;
  let inCode = false;

  for (const line of lines) {
    const trimmed = line.trim();

    if (trimmed.startsWith("```") || trimmed.startsWith("~~~")) {
      inCode = !inCode;
      offset += line.length + 1;
      continue;
    }

    if (!inCode) {
      const leading = line.length - line.replace(/^ */, "").length;

      if (leading <= 3) {
        const left = line.slice(leading);

        if (left.startsWith("#")) {
          let hashes = 0;

          while (hashes < left.length && left[hashes] === "#") {
            hashes += 1;
          }

          if (hashes > 0 && hashes <= 6) {
            const title = normalizeHeadingText(left.slice(hashes));

            if (title) {
              return markdown.slice(0, offset).trim();
            }
          }
        }
      }
    }

    offset += line.length + 1;
  }

  return markdown.trim();
}

function sourceHeading(line: string): { level: string; title: string } | null {
  const match = line.match(/^ {0,3}(#{1,6})(?:[ \t]+|$)(.*)$/);

  if (!match) return null;
  const title = normalizeHeadingText(match[2]);

  return title ? { level: `H${match[1].length}`, title } : null;
}

function sourceSectionIDs(source: string, sections: RenderedSection[]): Map<number, string> {
  const flat = flattenSections(sections).map(({ section }) => section);
  const result = new Map<number, string>();

  if (flat.length === 0) return result;

  let next = 0;
  let inFence = false;
  source.split("\n").forEach((line, lineIndex) => {
    const trimmed = line.trim();

    if (trimmed.startsWith("```") || trimmed.startsWith("~~~")) {
      inFence = !inFence;

      return;
    }

    if (inFence) return;
    const heading = sourceHeading(line);

    if (!heading || next >= flat.length) return;
    const candidate = flat[next];

    const candidateMatches =
      candidate &&
      candidate.level.toUpperCase() === heading.level &&
      outlineTitleKey(candidate.title) === outlineTitleKey(heading.title);

    if (candidateMatches) {
      result.set(lineIndex, candidate.id);
      next += 1;

      return;
    }

    const later = flat.findIndex(
      (section, index) =>
        index >= next &&
        section.level.toUpperCase() === heading.level &&
        outlineTitleKey(section.title) === outlineTitleKey(heading.title),
    );

    if (later >= next) {
      result.set(lineIndex, flat[later].id);
      next = later + 1;
    }
  });

  return result;
}

function ReadonlySource({
  source,
  sections,
  sectionRefs,
  targetLine,
}: {
  source: string;
  sections: RenderedSection[];
  sectionRefs: MutableRefObject<Map<string, HTMLElement>>;
  targetLine?: number;
}) {
  const sectionIDs = useMemo(() => sourceSectionIDs(source, sections), [sections, source]);
  const lines = source.split("\n");
  const targetRef = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (!targetRef.current) return;
    targetRef.current.scrollIntoView({ block: "center" });
    targetRef.current.focus({ preventScroll: true });
  }, [targetLine]);

  return (
    <pre className="ontology-source" aria-label="Note source">
      {lines.map((line, index) => (
        <span
          className={`ontology-source__line${targetLine === index + 1 ? " is-issue-target" : ""}`}
          data-source-line={index + 1}
          tabIndex={targetLine === index + 1 ? -1 : undefined}
          key={`${index}-${line}`}
          ref={(element) => {
            if (targetLine === index + 1) targetRef.current = element;
            const id = sectionIDs.get(index);

            if (!id) return;

            if (element) sectionRefs.current.set(id, element);
            else sectionRefs.current.delete(id);
          }}
        >
          <span className="ontology-source__number" aria-hidden="true">
            {index + 1}
          </span>
          <code>{line || " "}</code>
        </span>
      ))}
    </pre>
  );
}

type HeadingComponent = (tag: HeadingTag) => (props: { children?: ReactNode }) => ReactNode;

type HeadingTag = "h1" | "h2" | "h3" | "h4" | "h5" | "h6";

function headingTag(level: string | undefined): HeadingTag {
  switch (level?.toUpperCase()) {
    case "H1":
      return "h1";
    case "H3":
      return "h3";
    case "H4":
      return "h4";
    case "H5":
      return "h5";
    case "H6":
      return "h6";
    default:
      return "h2";
  }
}

type SectionWalkerProps = {
  sections: RenderedSection[];
  headingComponent: HeadingComponent;
  markdownComponents: MarkdownComponents;
  keyPrefix?: string;
};

/**
 * SectionWalker renders the note body as a depth-first preorder traversal of
 * the parsed section tree, using each section's own-content. It mirrors the
 * iteration order of `flattenSections` so `headingComponent` — which
 * increments a closure-scoped counter to bind DOM refs — stays in lockstep
 * with the outline's section-index map.
 *
 * Typed sections (those carrying `typeName` + `previewTemplate`) render
 * inside a collapsible `TypedSectionCard`; their children recurse *inside*
 * the card body so expanding/collapsing a parent hides every descendant.
 */
function SectionWalker({
  sections,
  headingComponent,
  markdownComponents,
  keyPrefix = "",
}: SectionWalkerProps) {
  return (
    <>
      {sections.map((section, index) => {
        const key = `${keyPrefix}${section.id || index}`;
        const level = headingTag(section.level);
        const Heading = headingComponent(level);
        const ownContent = convertWikilinks(section.content || "");

        const bodyMarkdown = ownContent ? (
          <Markdown urlTransform={(url) => url} components={markdownComponents}>
            {ownContent}
          </Markdown>
        ) : null;

        const childWalker =
          section.children && section.children.length > 0 ? (
            <SectionWalker
              sections={section.children}
              headingComponent={headingComponent}
              markdownComponents={markdownComponents}
              keyPrefix={`${key}-`}
            />
          ) : null;

        const isTyped = Boolean(section.typeName && section.previewTemplate);

        return (
          <section key={key} className="ontology-section">
            <Heading>{displayTitle(section.title)}</Heading>
            {isTyped ? (
              <TypedSectionCard section={section} markdownComponents={markdownComponents}>
                {bodyMarkdown}
                {childWalker}
              </TypedSectionCard>
            ) : (
              <>
                {bodyMarkdown}
                {childWalker}
              </>
            )}
          </section>
        );
      })}
    </>
  );
}

// Headings keep their Markdown; note titles arrive as plain text. Compare the
// text a reader sees so an H1 like `[[Sync]] 2022` still matches its title.
function outlineTitleKey(value: string | undefined): string {
  return displayTitle(normalizeHeadingText(value || "")).toLocaleLowerCase();
}

function outlineSectionNotePath(section: RenderedSection): string {
  if (section.notePath) return section.notePath;

  return section.id.split("#", 1)[0] || "";
}

function isCurrentFileWrapper(section: RenderedSection): boolean {
  return section.locator?.toUpperCase() === "FILE";
}

function isCurrentNoteIdentityRoot(
  section: RenderedSection,
  currentNotePath: string,
  currentNoteTitle: string,
): boolean {
  return (
    section.locator?.toUpperCase() === "NOTE" &&
    (!currentNotePath || outlineSectionNotePath(section) === currentNotePath) &&
    outlineTitleKey(section.title) !== "" &&
    outlineTitleKey(section.title) === outlineTitleKey(currentNoteTitle)
  );
}

function isIdentityH1(
  section: RenderedSection,
  currentNotePath: string,
  currentNoteTitle: string,
): boolean {
  return (
    section.level.toUpperCase() === "H1" &&
    (!currentNotePath || outlineSectionNotePath(section) === currentNotePath) &&
    outlineTitleKey(section.title) !== "" &&
    outlineTitleKey(section.title) === outlineTitleKey(currentNoteTitle)
  );
}

/**
 * Project the outline for display without changing any node identity. A note
 * FILE node is a transport/container wrapper, and a direct H1 that repeats the
 * note title is its Markdown counterpart. Only those two presentation layers
 * are promoted; nested and duplicate-titled nodes remain in their authored
 * order with their canonical ids.
 */
function projectOutlineRoots(
  sections: RenderedSection[],
  currentNotePath?: string | null,
  currentNoteTitle?: string | null,
): RenderedSection[] {
  if (sections.length === 0) return sections;

  let roots = sections;

  const fileIndex = roots.findIndex(
    (section) =>
      isCurrentFileWrapper(section) ||
      isCurrentNoteIdentityRoot(section, currentNotePath || "", currentNoteTitle || ""),
  );

  if (fileIndex >= 0) {
    const file = roots[fileIndex];
    roots = [...roots.slice(0, fileIndex), ...(file.children || []), ...roots.slice(fileIndex + 1)];
  }

  if (!currentNoteTitle) return roots;

  // Remove only the first direct identity H1. A second H1 with the same title
  // is a separate canonical target and must remain visible.
  const identityIndex = roots.findIndex((section) =>
    isIdentityH1(section, currentNotePath || "", currentNoteTitle),
  );

  if (identityIndex < 0) {
    return roots;
  }

  const identity = roots[identityIndex];

  return [
    ...roots.slice(0, identityIndex),
    ...(identity.children || []),
    ...roots.slice(identityIndex + 1),
  ];
}

function structuralNodeLabel(node: StructuralNode): string {
  return node.label || node.title || publicTypeName(node.typeName) || "Untitled";
}

function parseAnchorFromPath(path: string | null | undefined): string | null {
  if (!path) return null;
  const idx = path.indexOf("#");

  if (idx === -1 || idx === path.length - 1) return null;

  return path.slice(idx + 1);
}

function structuralNodeBadge(node: StructuralNode): string | null {
  const directListChildren = structuralNodeDirectListChildren(node);

  if (directListChildren.length > 0) {
    return String(directListChildren.length);
  }

  return null;
}

function structuralNodeOwnMarkdown(node: StructuralNode): string {
  return convertWikilinks(node.content || "");
}

function structuralNodeIsList(node: StructuralNode): boolean {
  return Boolean(node.list);
}

function structuralNodeDirectListChildren(node: StructuralNode): StructuralNode[] {
  const children = node.children || [];

  if (children.length === 0) return [];

  if (structuralNodeIsList(node)) return children;

  return children.filter((child) => child.list || child.fieldList);
}

function structuralNodeIsDeclaredSection(node: StructuralNode): boolean {
  return Boolean(node.fieldName || node.fieldPath);
}

function structuralNodeDisplaysInline(node: StructuralNode): boolean {
  return node.sectionDisplay === "INLINE";
}

function structuralNodeOpensPane(node: StructuralNode): boolean {
  return structuralNodeIsDeclaredSection(node) && !structuralNodeDisplaysInline(node);
}

function findStructuralMatch(
  roots: StructuralNode[],
  predicate: (node: StructuralNode) => boolean,
): { root: StructuralNode; node: StructuralNode } | null {
  for (const root of roots) {
    const queue = [root];

    while (queue.length > 0) {
      const current = queue.shift();

      if (!current) continue;

      if (predicate(current)) return { root, node: current };
      queue.push(...(current.children || []));
    }
  }

  return null;
}

function structuralNodeToOutlineSection(node: StructuralNode): RenderedSection {
  return {
    id: node.nodeId,
    title: structuralNodeLabel(node),
    level: node.level || "H2",
    content: node.content || "",
    notePath: node.notePath,
    parentId: node.parentNodeId,
    locator: node.locator,
    children: (node.children || []).map(structuralNodeToOutlineSection),
  };
}

function StructuralNodeView({
  rootNodes,
  initialAnchor,
  sectionRefs,
  onOpenNode,
  markdownComponents,
}: {
  rootNodes: StructuralNode[];
  initialAnchor?: string | null;
  sectionRefs: MutableRefObject<Map<string, HTMLElement>>;
  onOpenNode?: (node: StructuralNode) => void;
  markdownComponents: MarkdownComponents;
}) {
  // Handle initialAnchor: scroll to matching section. Latch by anchor so we
  // only scroll once per anchor change because `rootNodes` gets a fresh array
  // reference on every parent re-render. Repeating this effect mid-scroll
  // would pull the reading surface back to the anchor.
  const lastScrolledAnchor = useRef<string | null>(null);
  useEffect(() => {
    if (!initialAnchor) {
      lastScrolledAnchor.current = null;

      return;
    }

    if (lastScrolledAnchor.current === initialAnchor) return;

    const needle = initialAnchor.startsWith("^")
      ? initialAnchor
      : initialAnchor.startsWith("#^")
        ? initialAnchor.slice(1)
        : initialAnchor;

    const match = findStructuralMatch(rootNodes, (current) => {
      const currentID = current.nodeId || "";

      return (
        currentID === needle ||
        currentID.endsWith(`#${needle}`) ||
        currentID.endsWith(`#${initialAnchor}`)
      );
    });

    if (match) {
      lastScrolledAnchor.current = initialAnchor;
      requestAnimationFrame(() => {
        sectionRefs.current
          .get(match.root.nodeId)
          ?.scrollIntoView({ behavior: scrollBehavior(), block: "start" });
      });
    }
  }, [initialAnchor, rootNodes, sectionRefs]);

  if (rootNodes.length === 0) return null;

  return (
    <div className="ontology-struct ontology-struct--vertical">
      <div className="ontology-struct__sections">
        {rootNodes.map((root) => {
          const ownMarkdown = structuralNodeOwnMarkdown(root);
          const rootListNodes = structuralNodeDirectListChildren(root);

          const childSections =
            root.locator === "FILE" || root.locator === "SECTION"
              ? (root.children || []).filter(
                  (child) => !rootListNodes.some((item) => item.nodeId === child.nodeId),
                )
              : [];

          const renderChildSubtree = (node: StructuralNode): ReactNode => {
            const nodeOwnMarkdown = structuralNodeOwnMarkdown(node);
            const nodeListNodes = structuralNodeDirectListChildren(node);

            const nodeChildSections = (node.children || []).filter(
              (child) => !nodeListNodes.some((item) => item.nodeId === child.nodeId),
            );

            const nodeBadge = structuralNodeBadge(node);

            return (
              <section
                key={node.nodeId}
                className="ontology-struct__child-section"
                ref={(el) => {
                  if (el) sectionRefs.current.set(node.nodeId, el);
                  else sectionRefs.current.delete(node.nodeId);
                }}
              >
                <h3 className="ontology-struct__section-title">
                  <span>{structuralNodeLabel(node)}</span>
                  {nodeBadge && <span className="ontology-struct__section-badge">{nodeBadge}</span>}
                </h3>

                {nodeOwnMarkdown && (
                  <div className="ontology-struct__section-body">
                    <Markdown urlTransform={(url) => url} components={markdownComponents}>
                      {nodeOwnMarkdown}
                    </Markdown>
                  </div>
                )}

                {nodeListNodes.length > 0 && (
                  <div className="ontology-struct__section-collection">
                    <div className="ontology-struct__section-collection-label">Included nodes</div>
                    <ul className="ontology-struct__section-list">
                      {nodeListNodes.map((child) => (
                        <li key={child.nodeId}>
                          <button
                            type="button"
                            className="ontology-struct__section-list-item"
                            onClick={() => onOpenNode?.(child)}
                          >
                            <span className="ontology-struct__section-list-title">
                              {structuralNodeLabel(child)}
                            </span>
                            {publicTypeName(child.typeName) && (
                              <span className="ontology-struct__section-list-meta">
                                {publicTypeName(child.typeName)}
                              </span>
                            )}
                          </button>
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {nodeChildSections.length > 0 && (
                  <div className="ontology-struct__children">
                    {nodeChildSections.map((child) => {
                      if (!structuralNodeOpensPane(child)) {
                        return renderChildSubtree(child);
                      }

                      const childBadge = structuralNodeBadge(child);

                      return (
                        <section
                          key={child.nodeId}
                          className="ontology-struct__child-section"
                          ref={(el) => {
                            if (el) sectionRefs.current.set(child.nodeId, el);
                            else sectionRefs.current.delete(child.nodeId);
                          }}
                        >
                          <h3 className="ontology-struct__section-title">
                            <button
                              type="button"
                              className="ontology-struct__section-heading-link"
                              onClick={() => onOpenNode?.(child)}
                            >
                              <span>{structuralNodeLabel(child)}</span>
                              {childBadge && (
                                <span className="ontology-struct__section-badge">{childBadge}</span>
                              )}
                            </button>
                          </h3>
                        </section>
                      );
                    })}
                  </div>
                )}
              </section>
            );
          };

          return (
            <section
              key={root.nodeId}
              className="ontology-struct__section"
              ref={(el) => {
                if (el) sectionRefs.current.set(root.nodeId, el);
                else sectionRefs.current.delete(root.nodeId);
              }}
            >
              {ownMarkdown && (
                <div className="ontology-struct__section-body">
                  <Markdown urlTransform={(url) => url} components={markdownComponents}>
                    {ownMarkdown}
                  </Markdown>
                </div>
              )}

              {rootListNodes.length > 0 && (
                <div className="ontology-struct__section-collection">
                  <div className="ontology-struct__section-collection-label">Included nodes</div>
                  <ul className="ontology-struct__section-list">
                    {rootListNodes.map((child) => (
                      <li key={child.nodeId}>
                        <button
                          type="button"
                          className="ontology-struct__section-list-item"
                          onClick={() => onOpenNode?.(child)}
                        >
                          <span className="ontology-struct__section-list-title">
                            {structuralNodeLabel(child)}
                          </span>
                          {publicTypeName(child.typeName) && (
                            <span className="ontology-struct__section-list-meta">
                              {publicTypeName(child.typeName)}
                            </span>
                          )}
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {childSections.length > 0 && (
                <div className="ontology-struct__children">
                  {childSections.map((child) => {
                    if (!structuralNodeOpensPane(child)) {
                      return renderChildSubtree(child);
                    }

                    const childBadge = structuralNodeBadge(child);

                    return (
                      <section
                        key={child.nodeId}
                        className="ontology-struct__child-section"
                        ref={(el) => {
                          if (el) sectionRefs.current.set(child.nodeId, el);
                          else sectionRefs.current.delete(child.nodeId);
                        }}
                      >
                        <h3 className="ontology-struct__section-title">
                          <button
                            type="button"
                            className="ontology-struct__section-heading-link"
                            onClick={() => onOpenNode?.(child)}
                          >
                            <span>{structuralNodeLabel(child)}</span>
                            {childBadge && (
                              <span className="ontology-struct__section-badge">{childBadge}</span>
                            )}
                          </button>
                        </h3>
                      </section>
                    );
                  })}
                </div>
              )}
            </section>
          );
        })}
      </div>
    </div>
  );
}

export function OntologyNotePane({
  loading,
  error,
  onRetry,
  rendered: renderedProp = null,
  workspace,
  initialAnchor: initialAnchorProp,
  documentQuery,
  applicationHref,
  onOpen,
  onOpenNode,
  onStageOps,
  vaultKey = null,
  onContextChange,
  viewMode = "read",
  onViewModeChange,
  presentationControls,
  renderPresentation,
  contextCollapsed,
  onToggleContext,
  breadcrumbs,
  editing = false,
  editSession,
  issueTarget,
  issueFallback = false,
  validationIssueCount,
  validationHealth,
  onOpenIssues,
}: Props) {
  const paneRef = useRef<HTMLElement | null>(null);
  const [focusUnavailable, setFocusUnavailable] = useState(false);
  const canonicalWorkspace = workspace;
  const locator = useMemo(() => focusedNodeLocator(canonicalWorkspace), [canonicalWorkspace]);
  const [copyLinkMessage, setCopyLinkMessage] = useState<string | null>(null);
  const content = canonicalWorkspace?.content || null;
  const renderedBase = content?.rendered || renderedProp || null;

  const renderedSections = useMemo(
    () => selectWorkspaceRenderedSections(canonicalWorkspace, renderedBase),
    [canonicalWorkspace, renderedBase],
  );

  const rendered = useMemo(() => {
    if (!renderedBase) return null;

    return {
      ...renderedBase,
      sections: renderedSections,
    };
  }, [renderedBase, renderedSections]);

  const assessment = content?.assessment || null;

  const workspaceNodeRef = useMemo(
    () => nodeRefLikeForWorkspace(canonicalWorkspace, rendered),
    [canonicalWorkspace, rendered],
  );

  const stagedTarget = useMemo(
    () => ({ ref: workspaceNodeRef, paths: [rendered?.path] }),
    [rendered?.path, workspaceNodeRef],
  );

  const changedFieldNames = useMemo(
    () => changedFields(editSession, stagedTarget),
    [editSession, stagedTarget],
  );

  const dirtyCollectionNames = useMemo(
    () => changedCollections(editSession, stagedTarget),
    [editSession, stagedTarget],
  );

  const handleCopyLocatorLink = async () => {
    setCopyLinkMessage(null);

    if (!locator) {
      setCopyLinkMessage(locatorDiagnostic(null));

      return;
    }

    if (locator.status === "linkable" && locator.linkTarget?.wikilink) {
      try {
        await navigator.clipboard.writeText(locator.linkTarget.wikilink);
        setCopyLinkMessage("Copied link.");
      } catch {
        setCopyLinkMessage("Clipboard unavailable.");
      }

      return;
    }

    if (locator.status === "requires_fix") {
      const ops =
        canonicalWorkspace?.linkFixOps && canonicalWorkspace.linkFixOps.length > 0
          ? canonicalWorkspace.linkFixOps
          : locator.fixActions?.map((action) => ({
              kind: "ensureBlockID",
              path: action.ref.notePath,
              nodeId: action.ref.nodeId,
              structuralFingerprint: action.ref.structuralFingerprint,
              blockId: action.blockId,
            }));

      if (ops && ops.length > 0 && onStageOps) {
        try {
          await onStageOps(ops);
          setCopyLinkMessage("Block ID fix staged.");
        } catch {
          setCopyLinkMessage("Could not stage the Block ID fix.");
        }
      } else {
        setCopyLinkMessage(locatorDiagnostic(locator));
      }

      return;
    }

    setCopyLinkMessage(locatorDiagnostic(locator));
  };

  // Canonical field-node graph (with schema typeName + enumValues) feeds the
  // identity strip and property panel directly.
  const workspaceFieldNodes = useMemo<WorkspaceFieldNode[]>(() => {
    const focusedID = canonicalWorkspace?.focusedNodeId || "";

    const fields = (canonicalWorkspace?.nodes || []).filter(
      (node): node is WorkspaceFieldNode =>
        node.kind === "field" && (!focusedID || node.parentId === focusedID),
    );

    return applyDirtyToFieldNodes(fields, changedFieldNames);
  }, [canonicalWorkspace?.focusedNodeId, canonicalWorkspace?.nodes, changedFieldNames]);

  const identityPartition = useMemo(
    () => pickIdentityFields(workspaceFieldNodes),
    [workspaceFieldNodes],
  );

  // Focused workspace node — where BodyWalker roots its render. The server
  // publishes `focusedNodeId` + canonical `nodes[]` in every response; this
  // just wires them together for the client walker.
  const focusedWorkspaceNode = useMemo(() => {
    const nodes = canonicalWorkspace?.nodes;
    const id = canonicalWorkspace?.focusedNodeId;

    if (!nodes || !id) return null;

    return nodes.find((node) => node.id === id) || null;
  }, [canonicalWorkspace]);

  const effectiveStatus = useMemo(() => {
    const base = canonicalWorkspace?.status || {
      dirty: false,
      validation: { issueCount: 0 },
      freshness: {},
      session: {},
      hasWarnings: false,
    };

    const touched = isTouched(editSession, stagedTarget);

    const fieldDirty = Array.from(changedFieldNames).length > 0;
    const collectionDirty = Array.from(dirtyCollectionNames).length > 0;

    const hasDirtySession =
      Boolean(editSession?.hasUncommittedChanges) ||
      editSession?.status === "dirty" ||
      editSession?.status === "rebased" ||
      editSession?.status === "conflicted";

    const hasSessionIndicator = hasDirtySession || editSession?.status === "stale";

    const sessionState =
      touched && hasSessionIndicator && editSession?.status ? { state: editSession.status } : {};

    const freshnessState =
      rendered?.path && (editSession?.stalePaths || []).includes(rendered.path)
        ? {
            state:
              editSession?.status === "rebased" ||
              editSession?.status === "conflicted" ||
              editSession?.status === "stale"
                ? editSession.status
                : "stale",
          }
        : {};

    return mergeStatus(base, {
      dirty: base.dirty || fieldDirty || collectionDirty || (hasDirtySession && touched),
      session: sessionState,
      freshness: freshnessState,
      hasWarnings: base.hasWarnings || (base.validation?.issueCount || 0) > 0,
    });
  }, [
    canonicalWorkspace?.status,
    changedFieldNames,
    dirtyCollectionNames,
    editSession,
    stagedTarget,
  ]);

  const initialAnchor = initialAnchorProp ?? parseAnchorFromPath(rendered?.path);
  const structural = selectWorkspaceStructuralView(canonicalWorkspace);
  const structuralTabs = useMemo(() => structural?.tabs || [], [structural?.tabs]);
  const structuralRoot = structural?.root || null;

  const structuralNodes = useMemo(
    () => (structuralRoot ? [structuralRoot] : structuralTabs.flatMap((tab) => tab.nodes || [])),
    [structuralRoot, structuralTabs],
  );

  const useStructuralMode = viewMode === "read" && structuralNodes.length > 0;
  const headingRefs = useRef<Array<HTMLElement | null>>([]);
  const structuralSectionRefs = useRef<Map<string, HTMLElement>>(new Map());
  const sourceSectionRefs = useRef<Map<string, HTMLElement>>(new Map());
  const sourceEditorRef = useRef<MarkdownEditorHandle | null>(null);

  const currentWorkspaceIsNote = canonicalWorkspace?.node.ref.kind?.toUpperCase() === "NOTE";

  const currentNotePath = rendered?.path
    ? notePathFromLocator(rendered.path)
    : currentWorkspaceIsNote
      ? canonicalWorkspace.node.notePath || canonicalWorkspace.content.path
      : null;

  const currentNoteTitle = rendered?.title
    ? rendered.title
    : currentWorkspaceIsNote
      ? canonicalWorkspace.content.title || canonicalWorkspace.node.title
      : null;

  const outlineRoots = useMemo(
    () =>
      projectOutlineRoots(
        useStructuralMode
          ? structuralNodes.map(structuralNodeToOutlineSection)
          : rendered?.sections || [],
        currentNotePath,
        currentNoteTitle,
      ),
    [currentNotePath, currentNoteTitle, rendered?.sections, structuralNodes, useStructuralMode],
  );

  const resolvedType = publicTypeName(canonicalWorkspace?.content.resolvedType);
  const typeLabel = useTypeLabel();

  const toolbarPath =
    canonicalWorkspace?.node.notePath || canonicalWorkspace?.content.path || rendered?.path || "";

  const isActiveHTML =
    content?.format === "html" &&
    content.sourceCapabilities?.includes("active_content_viewing") === true &&
    Boolean(toolbarPath);

  const isMarkdownSource = !content?.format || content.format === "markdown";
  const sourceText = content?.markdown || rendered?.rendered || "";

  const sourceSections = useMemo(
    () => (rendered?.sections?.length ? rendered.sections : outlineRoots),
    [outlineRoots, rendered?.sections],
  );

  const sourceSectionLines = useMemo(() => {
    const lines = new Map<string, number>();
    sourceSectionIDs(sourceText, sourceSections).forEach((id, line) => lines.set(id, line));

    return lines;
  }, [sourceSections, sourceText]);

  const sourceLinks = useRhizomeMarkdownLinks({
    currentPath: toolbarPath,
    rendered,
    onOpen,
  });

  // Map each rendered section by id to its document-order index so the outline
  // can resolve a promoted section against the unchanged markdown DOM.
  const sectionIndexMap = useMemo(() => {
    const map = new Map<string, number>();
    const bodySections = useStructuralMode ? [] : flattenSections(rendered?.sections || []);
    bodySections.forEach(({ section }, index) => {
      if (section.id) map.set(section.id, index);
    });

    return map;
  }, [rendered?.sections, useStructuralMode]);

  const markdown = useMemo(() => {
    if (!rendered) return "";

    return convertWikilinks(rendered.rendered || "");
  }, [rendered]);

  const prefaceMarkdown = useMemo(() => extractPrefaceMarkdown(markdown), [markdown]);

  // Use the section walker only when at least one section is typed; plain
  // notes stay on the single-markdown path so pre-heading content (anything
  // before the first H*) still renders.
  const useSectionWalker = useMemo(
    () => anySectionTyped(rendered?.sections || []),
    [rendered?.sections],
  );

  // issueCount must SUM across top-level, field, and relation issues,
  // not short-circuit on the first truthy source.
  const issueCount = useMemo(() => {
    if (canonicalWorkspace) {
      return effectiveStatus.validation.issueCount || 0;
    }

    if (!assessment) return 0;
    const top = assessment.issues?.length ?? 0;

    const fields = (assessment.fields ?? []).reduce(
      (sum, field) => sum + (field.issues?.length ?? 0),
      0,
    );

    const relations = (assessment.relations ?? []).reduce(
      (sum, relation) => sum + (relation.issues?.length ?? 0),
      0,
    );

    return top + fields + relations;
  }, [assessment, canonicalWorkspace, effectiveStatus.validation.issueCount]);

  // Group issues by origin so the expandable block can render them with a
  // readable hierarchy: top-level note issues, then per-field and
  // per-relation issues labeled by the field/relation name.
  const issueSections = useMemo(() => {
    const sections: Array<{
      key: string;
      label: string;
      issues: Array<{ code?: string; message?: string }>;
    }> = [];

    if (!assessment) return sections;

    if (assessment.issues && assessment.issues.length > 0) {
      sections.push({
        key: "top",
        label: "Note",
        issues: assessment.issues,
      });
    }

    (assessment.fields ?? []).forEach((field) => {
      if (field.issues && field.issues.length > 0) {
        sections.push({
          key: `field-${field.name ?? ""}`,
          label: field.name ? `Field · ${field.name}` : "Field",
          issues: field.issues,
        });
      }
    });
    (assessment.relations ?? []).forEach((relation) => {
      if (relation.issues && relation.issues.length > 0) {
        sections.push({
          key: `relation-${relation.name ?? ""}`,
          label: relation.name ? `Relation · ${relation.name}` : "Relation",
          issues: relation.issues,
        });
      }
    });

    return sections;
  }, [assessment]);

  const markdownComponents = useMemo<MarkdownComponents>(
    () => ({
      a: ({ href, children }) => {
        const target = resolveRenderedLinkTarget(href, rendered);

        if (target) {
          return (
            <NoteLinkPreview target={target} from={toolbarPath} open={onOpen}>
              {children}
            </NoteLinkPreview>
          );
        }

        return (
          <a href={href} target="_blank" rel="noreferrer">
            {children}
          </a>
        );
      },
    }),
    [onOpen, rendered, toolbarPath],
  );

  let headingIndex = 0;

  const headingComponent = (tag: "h1" | "h2" | "h3" | "h4" | "h5" | "h6") =>
    function Heading({ children }: { children?: ReactNode }) {
      const current = headingIndex++;

      return createElement(
        tag,
        {
          ref: (node: HTMLElement | null) => {
            headingRefs.current[current] = node;
          },
        },
        children,
      );
    };

  // The element that renders an outline section in the current view, if any.
  const outlineTarget = useCallback(
    (section: RenderedSection): HTMLElement | null => {
      const index = sectionIndexMap.get(section.id);

      return (
        structuralSectionRefs.current.get(section.id) ??
        sourceSectionRefs.current.get(section.id) ??
        (index === undefined ? null : headingRefs.current[index]) ??
        null
      );
    },
    [sectionIndexMap],
  );

  const outlineParents = useMemo(() => {
    const parents = new Map<string, RenderedSection>();

    const visit = (section: RenderedSection) =>
      section.children?.forEach((child) => {
        parents.set(child.id, section);
        visit(child);
      });

    outlineRoots.forEach(visit);

    return parents;
  }, [outlineRoots]);

  // The outline moves within this note. A section that renders elsewhere
  // (inside a pane-only parent) scrolls to its nearest rendered ancestor.
  const navigateOutline = useCallback(
    (section: RenderedSection) => {
      const sourceLine = sourceSectionLines.get(section.id);

      if (!structuralSectionRefs.current.has(section.id) && sourceEditorRef.current) {
        if (sourceLine !== undefined) sourceEditorRef.current.revealLine(sourceLine + 1);

        return;
      }

      for (
        let current: RenderedSection | undefined = section;
        current;
        current = outlineParents.get(current.id)
      ) {
        const target = outlineTarget(current);

        if (target) {
          target.scrollIntoView({ behavior: scrollBehavior(), block: "start" });

          return;
        }
      }
    },
    [outlineParents, outlineTarget, sourceSectionLines],
  );

  const tabContext = useMemo<NoteTabContext | null>(
    () =>
      canonicalWorkspace
        ? {
            workspace: canonicalWorkspace,
            outline: { sections: outlineRoots, navigate: navigateOutline, target: outlineTarget },
            openNode: onOpenNode,
          }
        : null,
    [canonicalWorkspace, navigateOutline, onOpenNode, outlineRoots, outlineTarget],
  );

  useEffect(() => {
    onContextChange?.(tabContext);
  }, [onContextChange, tabContext]);
  useEffect(() => () => onContextChange?.(null), [onContextChange]);

  const sourceTargetRange = issueTarget ? issueTargetToTextRange(sourceText, issueTarget) : null;

  const sourceTargetLine =
    issueTarget?.unit === "line" && issueTarget.start !== undefined
      ? Math.max(1, issueTarget.start)
      : issueTarget?.unit === "utf8_bytes" && issueTarget.start !== undefined
        ? utf8ByteRangeToLineRange(
            sourceText,
            issueTarget.start,
            issueTarget.end ?? issueTarget.start,
          ).startLine
        : sourceTargetRange
          ? sourceText.slice(0, sourceTargetRange.from).split(/\r\n|\n/).length
          : undefined;

  useEffect(() => {
    const pane = paneRef.current;

    if (!pane || !rendered || !issueTarget || viewMode === "source") return;
    let selector = "";

    if (issueTarget.field) selector = `[data-field="${CSS.escape(issueTarget.field)}"]`;
    else if (issueTarget.relation) {
      selector = `[data-relation="${CSS.escape(issueTarget.relation)}"]`;
    }

    if (!selector) return;
    const target = pane.querySelector<HTMLElement>(selector);
    setFocusUnavailable(!target);

    if (!target) return;
    const disclosure = target.closest("details");

    if (disclosure) disclosure.open = true;
    target.classList.add("is-issue-target");
    target.tabIndex = -1;
    target.scrollIntoView({ block: "center" });
    target.focus({ preventScroll: true });

    return () => target.classList.remove("is-issue-target");
  }, [issueTarget, rendered, viewMode]);

  const propertyPanel = viewMode === "read" &&
    canonicalWorkspace &&
    (workspaceFieldNodes.length > 0 || canonicalWorkspace.node.parentRef) && (
      <OntologyPropertyPanel
        workspace={canonicalWorkspace}
        fields={identityPartition.others}
        editing={editing}
        changedFieldNames={changedFieldNames}
        onStageOps={onStageOps}
        vaultKey={vaultKey}
        onOpen={onOpen}
        rendered={rendered}
        linkFrom={toolbarPath}
      />
    );

  return (
    <article className="ontology-pane" ref={paneRef}>
      <header className="ontology-pane__header">
        <div className="ontology-pane__controls">
          <span className={`ontology-pane__type type-chip${resolvedType ? "" : " is-untyped"}`}>
            {resolvedType ? typeLabel(resolvedType) : "Untyped"}
          </span>
          {toolbarPath && <span className="ontology-pane__toolbar-path">{toolbarPath}</span>}
          {structuralTabs.length > 0 && onOpenNode ? (
            <div className="ontology-pane__section-tabs" role="toolbar" aria-label="Sections">
              {structuralTabs.map((tab) => {
                const target = tab.nodes?.[0];

                const active = Boolean(
                  target?.nodeId && target.nodeId === canonicalWorkspace?.node.ref.nodeId,
                );

                return (
                  <button
                    key={tab.key}
                    type="button"
                    aria-pressed={active}
                    disabled={!target}
                    onClick={() => target && onOpenNode(target)}
                  >
                    {tab.label}
                    {tab.count > 0 ? ` ${tab.count}` : ""}
                  </button>
                );
              })}
            </div>
          ) : null}
          <div className="ontology-pane__controls-spacer" />
          {presentationControls ?? (
            <div
              className="ontology-pane__view-toggle segmented"
              role="group"
              aria-label="View mode"
            >
              <button
                type="button"
                aria-pressed={viewMode === "read"}
                className={viewMode === "read" ? "is-active" : ""}
                onClick={() => onViewModeChange?.("read")}
              >
                {isMarkdownSource ? "Structure" : "Preview"}
              </button>
              <button
                type="button"
                aria-pressed={viewMode === "source"}
                className={viewMode === "source" ? "is-active" : ""}
                onClick={() => onViewModeChange?.("source")}
              >
                {isMarkdownSource ? "Markdown" : "Source"}
              </button>
            </div>
          )}
          {sessionStateLabel(effectiveStatus.session.state) && (
            <span
              className={`ontology-chip${effectiveStatus.session.state === "conflicted" ? "" : " ontology-chip--warning"}`}
            >
              {sessionStateLabel(effectiveStatus.session.state)}
            </span>
          )}
          {locator && (
            <button
              type="button"
              className="ontology-link-button"
              onClick={(event) => {
                event.stopPropagation();
                void handleCopyLocatorLink();
              }}
              title={locatorDiagnostic(locator)}
            >
              {locator.status === "requires_fix" ? "Make linkable" : "Copy link"}
            </button>
          )}
          {copyLinkMessage && <span className="ontology-chip">{copyLinkMessage}</span>}
          {onToggleContext && !contextCollapsed ? (
            <button
              type="button"
              className="ontology-icon-btn ontology-pane__context-toggle"
              aria-label={contextCollapsed ? "Expand note context" : "Collapse note context"}
              aria-pressed={!contextCollapsed}
              onClick={onToggleContext}
            >
              <span aria-hidden="true">▥</span>
            </button>
          ) : null}
        </div>
      </header>

      {(issueFallback || focusUnavailable) && (
        <div className="ontology-pane__target-fallback" role="status">
          The exact issue location is no longer available. Showing the nearest note context.
        </div>
      )}

      {!rendered && breadcrumbs}
      {loading && !rendered && <div className="ontology-empty">Loading note workspace…</div>}
      {!loading && error && !rendered && (
        <div className="ontology-empty" role="alert">
          <div>{error}</div>
          {onRetry && (
            <button type="button" onClick={onRetry}>
              Retry
            </button>
          )}
        </div>
      )}
      {!loading && !error && !rendered && (
        <div className="ontology-empty ontology-empty--pane">
          <span className="ontology-empty__icon" aria-hidden="true" />
          <div>Pick a note to open the ontology workspace.</div>
          <div className="ontology-empty__hint">Search, filter, or click a type to find one.</div>
        </div>
      )}

      {rendered && (
        <div className="ontology-pane__body">
          <div
            className={`ontology-pane__main${isActiveHTML && viewMode === "read" ? " ontology-pane__main--html" : ""}`}
            role="presentation"
          >
            {breadcrumbs}
            {loading && (
              <div className="ontology-pane__refresh-status" role="status">
                Refreshing note…
              </div>
            )}
            {error && <div className="ontology-pane__refresh-status is-error">{error}</div>}
            {(renderPresentation ?? ((body) => body))(
              <>
                {viewMode === "read" &&
                  (canonicalWorkspace ? (
                    <OntologyIdentityStrip
                      workspace={canonicalWorkspace}
                      identity={identityPartition}
                      editing={editing}
                      changedFieldNames={changedFieldNames}
                      issueCount={validationIssueCount}
                      validationHealth={validationHealth}
                      onOpenIssues={onOpenIssues}
                      dirty={effectiveStatus.dirty}
                      onStageOps={onStageOps}
                      vaultKey={vaultKey}
                    />
                  ) : (
                    <div className="ontology-identity ontology-identity--placeholder">
                      <h2 className="ontology-identity__title">
                        {displayTitle(rendered.title) || "Untitled"}
                      </h2>
                    </div>
                  ))}
                {viewMode === "read" && !isActiveHTML && propertyPanel}
                {viewMode === "read" && isActiveHTML && rendered && (
                  <HTMLRootMetadataPanel
                    path={toolbarPath}
                    fallbackTitle={content?.title || rendered.title || ""}
                    frontmatter={rendered.frontmatter}
                    existingFields={new Set(workspaceFieldNodes.map((node) => node.field.name))}
                    editing={editing}
                    dirtyFields={changedFieldNames}
                    vaultKey={vaultKey}
                    sourceRevision={canonicalWorkspace?.sourceRevision}
                    onStageOps={onStageOps}
                  >
                    {propertyPanel}
                  </HTMLRootMetadataPanel>
                )}
                {viewMode === "read" && issueCount > 0 && issueSections.length > 0 && (
                  <details className="ontology-issues">
                    <summary className="ontology-issues__summary">
                      <span className="ontology-issues__count">{issueCount}</span>
                      <span className="ontology-issues__label">
                        issue{issueCount === 1 ? "" : "s"}
                      </span>
                    </summary>
                    <div className="ontology-issues__body">
                      {issueSections.map((section) => (
                        <div
                          key={section.key}
                          className="ontology-issues__group"
                          data-field={
                            section.key.startsWith("field-") ? section.key.slice(6) : undefined
                          }
                          data-relation={
                            section.key.startsWith("relation-") ? section.key.slice(9) : undefined
                          }
                        >
                          <div className="ontology-issues__group-label">{section.label}</div>
                          <ul className="ontology-issues__list">
                            {section.issues.map((issue) => (
                              <li
                                key={`${section.key}-${issue.code ?? ""}-${issue.message ?? ""}`}
                                className="ontology-issues__item"
                              >
                                {issue.code && (
                                  <span className="ontology-issues__code">{issue.code}</span>
                                )}
                                <span className="ontology-issues__message">
                                  {issue.message || issue.code || "Unknown issue"}
                                </span>
                              </li>
                            ))}
                          </ul>
                        </div>
                      ))}
                    </div>
                  </details>
                )}

                <div
                  className={`ontology-markdown${isActiveHTML ? " ontology-markdown--html" : ""}`}
                >
                  {viewMode === "source" ? (
                    editing &&
                    canonicalWorkspace?.sourceRevision &&
                    content?.sourceCapabilities?.includes("structural_content_mutation") &&
                    onStageOps ? (
                      <SourceEditor
                        revision={canonicalWorkspace.sourceRevision}
                        vaultKey={vaultKey}
                        targetLine={sourceTargetLine}
                        rendered={rendered ?? canonicalWorkspace.content.rendered}
                        onOpen={onOpen}
                        onStageOps={onStageOps}
                      />
                    ) : isMarkdownSource ? (
                      <div className="ontology-source-view">
                        <MarkdownEditor
                          value={sourceText}
                          readOnly
                          ariaLabel="Note source"
                          documentId={`source:${encodeURIComponent(toolbarPath)}`}
                          revealLine={sourceTargetLine}
                          links={sourceLinks}
                          onChange={() => undefined}
                          editorRef={sourceEditorRef}
                        />
                      </div>
                    ) : (
                      <ReadonlySource
                        source={sourceText}
                        sections={sourceSections}
                        sectionRefs={sourceSectionRefs}
                        targetLine={sourceTargetLine}
                      />
                    )
                  ) : isActiveHTML ? null : viewMode === "read" &&
                    focusedWorkspaceNode &&
                    canonicalWorkspace &&
                    focusedWorkspaceNode.body?.some((block) => block.kind !== "inline_field") ? (
                    <BodyWalker
                      node={focusedWorkspaceNode}
                      context={
                        {
                          workspace: canonicalWorkspace,
                          mode: editing ? "edit" : "view",
                          vaultKey,
                          rendered,
                          onStageOps,
                          onOpen,
                          onOpenNode,
                          registerSectionTarget: (nodeId, element) => {
                            if (element) {
                              structuralSectionRefs.current.set(nodeId, element);
                            } else {
                              structuralSectionRefs.current.delete(nodeId);
                            }
                          },
                          lookupNode: (id) =>
                            canonicalWorkspace.nodes?.find((entry) => entry.id === id),
                        } satisfies BodyRenderContext
                      }
                    />
                  ) : useStructuralMode ? (
                    <StructuralNodeView
                      rootNodes={structuralNodes}
                      initialAnchor={initialAnchor}
                      sectionRefs={structuralSectionRefs}
                      onOpenNode={onOpenNode}
                      markdownComponents={markdownComponents}
                    />
                  ) : useSectionWalker ? (
                    <>
                      {prefaceMarkdown && (
                        <Markdown urlTransform={(url) => url} components={markdownComponents}>
                          {prefaceMarkdown}
                        </Markdown>
                      )}
                      <SectionWalker
                        sections={rendered.sections || []}
                        headingComponent={headingComponent}
                        markdownComponents={markdownComponents}
                      />
                    </>
                  ) : viewMode === "read" &&
                    editing &&
                    canonicalWorkspace?.sourceRevision &&
                    onStageOps ? (
                    <EmptyNarrativeEditor
                      revision={canonicalWorkspace.sourceRevision}
                      nodeRef={canonicalWorkspace.node.ref}
                      vaultKey={vaultKey}
                      onStageOps={onStageOps}
                    />
                  ) : (
                    <Markdown
                      urlTransform={(url) => url}
                      components={{
                        h1: headingComponent("h1"),
                        h2: headingComponent("h2"),
                        h3: headingComponent("h3"),
                        h4: headingComponent("h4"),
                        h5: headingComponent("h5"),
                        h6: headingComponent("h6"),
                        ...markdownComponents,
                      }}
                    >
                      {markdown}
                    </Markdown>
                  )}
                  {isActiveHTML && (
                    <HTMLNoteViewer
                      path={toolbarPath}
                      query={documentQuery}
                      fragment={initialAnchor}
                      generation={canonicalWorkspace?.version}
                      visible={viewMode === "read"}
                      applicationHref={applicationHref || buildNoteWebHref(toolbarPath)}
                      onNavigate={(target, beside, authored) =>
                        onOpen(
                          resolveKnownRenderedLinkTarget(authored, rendered) || target,
                          beside ? "beside" : "current",
                        )
                      }
                    />
                  )}
                </div>

                {viewMode === "read" && rendered.embeds && rendered.embeds.length > 0 && (
                  <section className="ontology-inline-previews">
                    <div className="ontology-inline-previews__header">
                      <h3>Inline previews</h3>
                      <p className="ontology-muted">
                        Open embedded notes in this note tab or another tab.
                      </p>
                    </div>
                    <div className="ontology-preview-list">
                      {rendered.embeds.map((embed) => (
                        <div key={embed.target} className="ontology-preview-card">
                          <div>
                            <div className="ontology-preview-card__title">
                              {embed.title || embed.target}
                            </div>
                            {embed.preview && (
                              <p className="ontology-preview-card__body">{embed.preview}</p>
                            )}
                          </div>
                          <div className="ontology-preview-card__actions">
                            {embed.kind === "note" && (
                              <>
                                <button
                                  type="button"
                                  className="ontology-link-button ontology-link-button--primary"
                                  onClick={(event) =>
                                    openFromClick(event, embed.target, onOpen, "stack")
                                  }
                                >
                                  Open
                                </button>
                                <button
                                  type="button"
                                  className="ontology-link-button"
                                  onClick={(event) =>
                                    openFromClick(event, embed.target, onOpen, "current")
                                  }
                                >
                                  Open here
                                </button>
                              </>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  </section>
                )}
              </>,
            )}
          </div>
        </div>
      )}
    </article>
  );
}
