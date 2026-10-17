import { isPlainObject, isNodeRef, isString, type JsonObject } from "../api/parse";
import type { NodeRef } from "../api/types";
import { encodeURLFragment } from "../lib/noteDeepLink";
import { formatTypeName } from "../lib/typeNames";

export type ViewContext =
  | { kind: "type"; type: string }
  | { kind: "interface"; interface: string }
  | { kind: "group"; group: string }
  | { kind: "node"; type: string; ref: NodeRef }
  | { kind: "workspace" }
  | { kind: "standalone" };

/** View queries may return null for optional GraphQL ref fields. */
export type ViewNodeRef = {
  [K in keyof NodeRef]: undefined extends NodeRef[K] ? NodeRef[K] | null : NodeRef[K];
};

export type ViewContextInput =
  | Exclude<ViewContext, { kind: "node" }>
  | { kind: "node"; type: string; ref: ViewNodeRef };

export type ViewModuleProps = {
  view: { id: string; name: string };
  context: ViewContext;
  configuration?: JsonObject;
  /** Canonical vault identity for personal preference cache isolation. */
  vaultKey?: string;
  /** An explicit independent mount slot, omitted for the shared instance. */
  preferenceSlot?: string;
};

export type ViewInvocation = ViewModuleProps;

export type OpenNodeOptions = { view?: string; beside?: boolean };

export function isViewNodeRef(value: unknown): value is NodeRef {
  return isNodeRef(value) && !!value.notePath.trim() && !!value.kind.trim();
}

/* oxlint-disable anti-slop/no-unknown-parameters, anti-slop/no-known-value-widening -- View wire parsers validate external data before returning canonical types. */
/** Normalize only known optional ref fields, then apply the canonical guard. */
export function parseViewNodeRef(value: unknown): NodeRef | null {
  if (!isPlainObject(value)) return null;
  const ref = { ...value };

  for (const key of [
    "fragment",
    "nodeId",
    "typeName",
    "parentId",
    "structuralFingerprint",
    "startByte",
    "endByte",
  ]) {
    if (ref[key] === null || ref[key] === undefined) delete ref[key];
  }

  return isViewNodeRef(ref) ? ref : null;
}

export function parseViewContext(value: unknown): ViewContext | null {
  if (!isPlainObject(value)) return null;
  let context = value;

  if (value.kind === "node") {
    const ref = parseViewNodeRef(value.ref);

    if (!ref) return null;
    context = { ...value, ref };
  }

  return isViewContext(context) ? context : null;
}

/* oxlint-enable anti-slop/no-unknown-parameters, anti-slop/no-known-value-widening */

export function isViewContext(value: unknown): value is ViewContext {
  if (typeof value !== "object" || value === null || !("kind" in value)) return false;

  switch (value.kind) {
    case "standalone":
    case "workspace":
      return Object.keys(value).length === 1;
    case "type":
      return (
        Object.keys(value).length === 2 &&
        "type" in value &&
        isString(value.type) &&
        !!value.type.trim()
      );
    case "interface":
      return (
        Object.keys(value).length === 2 &&
        "interface" in value &&
        isString(value.interface) &&
        !!value.interface.trim()
      );
    case "group":
      return (
        Object.keys(value).length === 2 &&
        "group" in value &&
        isString(value.group) &&
        !!value.group.trim() &&
        value.group !== "*"
      );
    case "node":
      return (
        Object.keys(value).length === 3 &&
        "type" in value &&
        isString(value.type) &&
        !!value.type.trim() &&
        "ref" in value &&
        isViewNodeRef(value.ref)
      );
    default:
      return false;
  }
}

export function customViewHref(
  id: string,
  context: ViewContextInput = { kind: "standalone" },
  options: { hosted?: boolean } = {},
) {
  const path = `/views/${encodeURIComponent(id)}`;

  const params = new URLSearchParams();

  const parsedContext = parseViewContext(context);

  if (!parsedContext) throw new Error("Invalid view context");

  if (parsedContext.kind !== "standalone") params.set("context", JSON.stringify(parsedContext));

  if (options.hosted) params.set("hosted", "1");

  return params.size ? `${path}?${params}` : path;
}

export function nodeHref(ref: ViewNodeRef, options: OpenNodeOptions = {}) {
  const params = new URLSearchParams({ note: ref.notePath, kind: ref.kind });

  if (ref.nodeId) params.set("nodeId", ref.nodeId);

  if (ref.structuralFingerprint) params.set("structural", ref.structuralFingerprint);

  if (options.view) params.set("presentation", options.view);

  return `/notes?${params}${encodeURLFragment(ref.fragment ?? "")}`;
}

/** Workspace navigation for built-in choices and registered native or custom views. */
export function workspaceViewHref(id: string, context: ViewContextInput = { kind: "standalone" }) {
  if (context.kind === "node") return nodeHref(context.ref, { view: id });

  if (context.kind === "standalone") return `/notes?${new URLSearchParams({ view: id })}`;

  if (context.kind === "workspace") return `/notes?${new URLSearchParams({ presentation: id })}`;

  const path =
    context.kind === "group"
      ? `/notes/group/${encodeURIComponent(context.group)}`
      : `/notes/${formatTypeName(context.kind === "type" ? context.type : context.interface)}`;

  return `${path}?${new URLSearchParams({ presentation: id })}`;
}
