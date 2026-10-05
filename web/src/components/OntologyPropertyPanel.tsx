import {
  type KeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import type {
  NodeRef,
  NodeWorkspace,
  OntologyEditFieldValue,
  OntologyEditOp,
  RenderedFile,
  WorkspaceFieldNode,
} from "../api/types";
import { publicWorkspaceRef } from "../api/client";
import { resolveRenderedLinkTarget } from "../lib/content";
import { FieldEditor, fieldExpectedFromNode, fieldOperationTarget } from "./editing/FieldEditor";
import { FieldLinkValues, fieldLinksForValues } from "./FieldLinkValues";
import { NoteLinkPreview, useNotePreviewTrigger } from "./notePreview/NoteLinkPreview";
import { buildFieldEditOp, effectiveFieldCapability } from "./ontologyFieldPicking";
import { displayTitle, humanizeName } from "../lib/labels";

const DETAIL_DISCLOSURE_KEY_PREFIX = "rhizome:ontology-properties:details:v1:";

type Props = {
  workspace: NodeWorkspace;
  /** Fields NOT consumed by the identity strip. */
  fields: WorkspaceFieldNode[];
  editing: boolean;
  /** Field names that have unsaved changes in the active session. */
  changedFieldNames: Set<string>;
  /** Stage one or more ops against the active session; the result is not read here. */
  onStageOps?: (ops: OntologyEditOp[]) => Promise<void> | void;
  onOpen?: (path: string, target?: "current" | "stack" | "beside") => void;
  vaultKey?: string | null;
  /** Resolves wikilinks in field values the way the note body does. */
  rendered?: RenderedFile | null;
  linkFrom?: string;
};

/**
 * Structured properties panel for the root workspace note. Renders one row
 * per schema-declared field, picking a widget based on the field's TypeName
 * and EnumValues. Hides entirely when there are no fields to render so
 * untyped notes — which never populate field nodes — look unchanged.
 *
 * Section/collection field kinds are excluded here because they describe
 * structural containers rather than scalar values and belong in the body
 * region (phase 3 of the structural-view redesign).
 */
export function OntologyPropertyPanel({
  workspace,
  fields,
  editing,
  changedFieldNames,
  onStageOps,
  onOpen,
  vaultKey = null,
  rendered = null,
  linkFrom,
}: Props) {
  const orderedFields = orderPropertyFields(
    fields.filter(isScalarDisplayable),
    workspace.node.ref.kind === "EMBEDDED",
  );

  const keyFields = orderedFields.filter(
    (node) => effectiveFieldCapability(node).displayImportance === "KEY",
  );

  const normalFields = orderedFields.filter(
    (node) => effectiveFieldCapability(node).displayImportance === "NORMAL",
  );

  const detailFields = orderedFields.filter(
    (node) => effectiveFieldCapability(node).displayImportance === "DETAIL",
  );

  const forceDetailsOpen = detailFields.some(detailFieldMustStayVisible);
  const disclosureKey = detailDisclosureKey(workspace);
  const [detailsOpen, setDetailsOpen] = useState(() => readDetailDisclosure(disclosureKey));

  useEffect(() => {
    setDetailsOpen(readDetailDisclosure(disclosureKey));
  }, [disclosureKey]);

  const displayable = [
    ...keyFields,
    ...normalFields,
    ...(editing || detailsOpen || forceDetailsOpen ? detailFields : []),
  ];

  const parentRef = workspace.node.parentRef;
  const parentPath = parentRef ? publicWorkspaceRef(parentRef) : "";

  if (orderedFields.length === 0 && !parentPath) return null;

  const path = nodeRefPath(workspace.node.ref) || workspace.node.notePath || workspace.content.path;
  const nodeId = workspace.node.ref.nodeId;

  const stageFieldOp = (
    node: WorkspaceFieldNode,
    value: OntologyEditFieldValue,
    expected?: OntologyEditOp["expected"],
  ) => {
    if (!onStageOps) return;

    if (node.field.name === "locator") {
      return onStageOps([
        {
          kind: "setBlockID",
          path,
          nodeId,
          blockId: value.kind === "scalar" ? value.scalar : "",
        },
      ]);
    }

    return onStageOps([
      buildFieldEditOp(
        path,
        nodeId,
        node,
        value,
        workspace.sourceRevision?.contentFingerprint,
        workspace.sourceRevision?.content,
        expected,
      ),
    ]);
  };

  return (
    <section className="ontology-properties">
      <div className="ontology-properties__header">
        <h3 className="ontology-properties__label">Properties</h3>
        {!editing && !forceDetailsOpen && detailFields.length > 0 && (
          <button
            type="button"
            className="ontology-properties__details-toggle"
            aria-expanded={detailsOpen}
            onClick={() => {
              setDetailsOpen((open) => {
                const next = !open;

                writeDetailDisclosure(disclosureKey, next);

                return next;
              });
            }}
          >
            {detailsOpen ? "Hide details" : `${detailFields.length} more`}
          </button>
        )}
      </div>
      <dl className="ontology-properties__grid">
        {parentRef && parentPath && (
          <div className="ontology-properties__row">
            <dt className="ontology-properties__key">Parent</dt>
            <dd className="ontology-properties__value">
              <ParentPropertyLink
                parentRef={parentRef}
                title={workspace.node.parentTitle}
                from={linkFrom || path}
                onOpen={onOpen}
              />
            </dd>
          </div>
        )}
        {displayable.map((node) => (
          <PropertyRow
            key={node.id}
            node={node}
            editing={editing}
            dirty={changedFieldNames.has(node.field.name)}
            vaultKey={vaultKey}
            operationTarget={fieldOperationTarget(path, nodeId, node.field.name)}
            sourceHash={workspace.sourceRevision?.contentFingerprint}
            sourceContent={workspace.sourceRevision?.content}
            onStage={(values, expected) => stageFieldOp(node, values, expected)}
            links={
              onOpen
                ? { rendered, from: linkFrom || path, open: (target, mode) => onOpen(target, mode) }
                : undefined
            }
          />
        ))}
      </dl>
    </section>
  );
}

function ParentPropertyLink({
  parentRef,
  title,
  from,
  onOpen,
}: {
  parentRef: NodeRef;
  title?: string;
  from: string;
  onOpen?: Props["onOpen"];
}) {
  const target = publicWorkspaceRef(parentRef);

  const previewTrigger = useNotePreviewTrigger<HTMLButtonElement>({
    target,
    from,
    open: (path, mode) => onOpen?.(path, mode),
  });

  return (
    <>
      <button
        {...previewTrigger.triggerProps}
        type="button"
        className="ontology-properties__link"
        onClick={(event) => {
          previewTrigger.close();
          onOpen?.(target, event.metaKey || event.ctrlKey ? "beside" : "stack");
        }}
      >
        {displayTitle(title) || nodeRefPath(parentRef)}
      </button>
      {previewTrigger.preview}
    </>
  );
}

/**
 * We surface scalar/enum/date fields; section and link-list fields belong
 * elsewhere in the structural view. valueKind "section-ref" is produced when
 * the field's values are resolved via @contains section bindings.
 */
function isScalarDisplayable(node: WorkspaceFieldNode): boolean {
  return effectiveFieldCapability(node).valueKind !== "section";
}

function detailFieldMustStayVisible(node: WorkspaceFieldNode): boolean {
  const capability = effectiveFieldCapability(node);

  return (
    node.status.validation.issueCount > 0 ||
    (node.field.issues?.length ?? 0) > 0 ||
    (capability.required && !node.field.present)
  );
}

function detailDisclosureKey(workspace: NodeWorkspace): string {
  const typeName =
    workspace.node.resolvedType || workspace.node.ref.typeName || workspace.content.resolvedType;

  return typeName ? `${DETAIL_DISCLOSURE_KEY_PREFIX}${encodeURIComponent(typeName)}` : "";
}

function readDetailDisclosure(key: string): boolean {
  if (!key || typeof window === "undefined") return false;

  try {
    return window.sessionStorage.getItem(key) === "true";
  } catch {
    return false;
  }
}

function writeDetailDisclosure(key: string, open: boolean): void {
  if (!key || typeof window === "undefined") return;

  try {
    window.sessionStorage.setItem(key, String(open));
  } catch {
    // Session storage is optional in private browsing and embedded contexts.
  }
}

function orderPropertyFields(
  fields: WorkspaceFieldNode[],
  embedded: boolean,
): WorkspaceFieldNode[] {
  if (!embedded) return fields;

  return [...fields].sort((left, right) => {
    if (left.field.name === "locator") return -1;

    if (right.field.name === "locator") return 1;

    return 0;
  });
}

function PropertyRow({
  node,
  editing,
  dirty,
  vaultKey,
  operationTarget,
  sourceHash,
  sourceContent,
  onStage,
  links,
}: {
  node: WorkspaceFieldNode;
  editing: boolean;
  dirty: boolean;
  vaultKey: string | null;
  operationTarget: string;
  sourceHash?: string;
  sourceContent?: string;
  onStage: (
    value: OntologyEditFieldValue,
    expected?: OntologyEditOp["expected"],
  ) => Promise<void> | void;
  links?: PropertyLinks;
}) {
  const { name, values = [], present } = node.field;
  const value = values[0] || "";
  const isLocator = name === "locator";
  const relation = effectiveFieldCapability(node).valueKind === "relation";

  return (
    <div
      className={`ontology-properties__row${dirty ? " is-dirty" : ""}${present ? "" : " is-missing"}`}
      data-field={name}
    >
      <dt className="ontology-properties__key" title={name}>
        {humanizeName(name)}
      </dt>
      <dd className="ontology-properties__value">
        {links && !editing && relation && values.length > 0 ? (
          <span className={`widget-text${dirty ? " is-dirty" : ""}`}>
            <FieldLinkValues
              links={fieldLinksForValues(values, node.field.links)}
              from={links.from}
              open={links.open}
            />
          </span>
        ) : links && !editing && values.some((item) => WIKILINK.test(item)) ? (
          <span className={`widget-text${dirty ? " is-dirty" : ""}`}>
            {linkedText(values.join(", "), links)}
          </span>
        ) : isLocator ? (
          <LocatorWidget
            value={value}
            editing={editing}
            dirty={dirty}
            present={present}
            onStage={(next) => onStage({ kind: "scalar", scalar: next })}
            ariaLabel={name}
          />
        ) : (
          <FieldEditor
            node={node}
            editing={editing}
            dirty={dirty}
            vaultKey={vaultKey}
            operationTarget={operationTarget}
            expected={fieldExpectedFromNode(node, sourceHash, sourceContent)}
            onStage={onStage}
          />
        )}
      </dd>
    </div>
  );
}

type PropertyLinks = {
  rendered: RenderedFile | null;
  from: string;
  open: (target: string, mode: "stack" | "beside") => void;
};

// Non-global so .test() keeps no lastIndex state between calls.
const WIKILINK = /\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/;

/** Renders each wikilink in a read-only field value as a note link. */
function linkedText(text: string, links: PropertyLinks): ReactNode[] {
  const parts: ReactNode[] = [];
  let last = 0;

  for (const match of text.matchAll(new RegExp(WIKILINK, "g"))) {
    const raw = match[1].trim();

    const target =
      resolveRenderedLinkTarget(`rhizome://note/${encodeURIComponent(raw)}`, links.rendered) || raw;

    parts.push(text.slice(last, match.index));
    parts.push(
      <NoteLinkPreview key={match.index} target={target} from={links.from} open={links.open}>
        {(match[2] ?? raw).trim()}
      </NoteLinkPreview>,
    );
    last = match.index + match[0].length;
  }

  parts.push(text.slice(last));

  return parts;
}

function nodeRefPath(ref?: NodeRef): string {
  if (!ref?.notePath) return "";

  if (ref.fragment) return `${ref.notePath}#${ref.fragment}`;

  return ref.notePath;
}

function isValidLocator(value: string): boolean {
  return /^[A-Za-z0-9_-]+$/.test(value.trim());
}

function LocatorWidget({
  value,
  editing,
  dirty,
  present,
  onStage,
  ariaLabel,
}: {
  value: string;
  editing: boolean;
  dirty: boolean;
  present: boolean;
  onStage: (value: string) => void;
  ariaLabel: string;
}) {
  const [draft, setDraft] = useState(value);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const valid = isValidLocator(draft);

  useEffect(() => {
    if (document.activeElement !== inputRef.current) {
      setDraft(value);
    }
  }, [value]);

  const commit = useCallback(() => {
    const next = draft.trim();

    if (next === value || !isValidLocator(next)) return;
    onStage(next);
  }, [draft, onStage, value]);

  const handleKeyDown = useCallback(
    (event: KeyboardEvent<HTMLInputElement>) => {
      if (event.key === "Enter") {
        event.preventDefault();
        commit();
        inputRef.current?.blur();
      } else if (event.key === "Escape") {
        event.preventDefault();
        setDraft(value);
        inputRef.current?.blur();
      }
    },
    [commit, value],
  );

  if (!editing) {
    if (!present || value.trim() === "") {
      return <span className="widget-text widget-text--empty">(empty)</span>;
    }

    return <span className={`widget-text${dirty ? " is-dirty" : ""}`}>^{value}</span>;
  }

  return (
    <span className="ontology-properties__locator-edit">
      <span aria-hidden="true">^</span>
      <input
        ref={inputRef}
        type="text"
        className={`widget-text widget-text--input${dirty ? " is-dirty" : ""}`}
        value={draft}
        aria-label={ariaLabel}
        aria-invalid={!valid}
        pattern="[A-Za-z0-9_-]+"
        onChange={(event) => setDraft(event.target.value.replace(/^\^+/, ""))}
        onBlur={commit}
        onKeyDown={handleKeyDown}
      />
    </span>
  );
}
