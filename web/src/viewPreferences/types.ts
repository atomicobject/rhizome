import {
  isFiniteNumber,
  isJsonObject,
  isJsonValue,
  isString,
  type JsonObject,
  type JsonValue,
} from "../api/parse";
import type { NodeRef } from "../api/types";
import { isViewContext, type ViewContext } from "../views/context";

export type ViewPreferenceScope = {
  viewId: string;
  context: ViewContext;
  slot?: string;
  widgetSlot?: string;
};

export type ViewPreferenceResponse = {
  scope: ViewPreferenceScope;
  revision: number;
  values: JsonObject;
  migrationClosed: boolean;
};

export type PreferencePatch = { set?: JsonObject; unset?: string[] };

export type PreferenceMigration = {
  migrationId: string;
  values: JsonObject;
  acknowledged?: () => void;
};

export function canonicalPreferenceScope(scope: ViewPreferenceScope): ViewPreferenceScope {
  const context = scope.context;

  let canonical: ViewContext =
    context.kind === "type"
      ? { kind: "type", type: context.type }
      : context.kind === "interface"
        ? { kind: "interface", interface: context.interface }
        : context.kind === "group"
          ? { kind: "group", group: context.group }
          : context.kind === "standalone" || context.kind === "workspace"
            ? { kind: context.kind }
            : context;

  if (context.kind === "node") {
    const ref = context.ref;
    const kind = ref.kind.toUpperCase();

    const stableRef: NodeRef = {
      notePath: ref.notePath.replace(/\\/g, "/").replace(/^(\.\/)+/, ""),
      kind,
      typeName: context.type,
    };

    if (kind !== "NOTE") {
      if (kind === "SECTION" && ref.fragment) stableRef.fragment = ref.fragment.replace(/^#/, "");
      else if (ref.nodeId && kind !== "SECTION") stableRef.nodeId = ref.nodeId;
      else if (ref.structuralFingerprint)
        stableRef.structuralFingerprint = ref.structuralFingerprint;
      else if (ref.fragment) stableRef.fragment = ref.fragment.replace(/^#/, "");
    }

    canonical = { kind: "node", type: context.type, ref: stableRef };
  }

  const result: ViewPreferenceScope = { viewId: scope.viewId, context: canonical };

  if (scope.slot) result.slot = scope.slot;

  if (scope.widgetSlot) result.widgetSlot = scope.widgetSlot;

  return result;
}

export function preferenceScopeKey(scope: ViewPreferenceScope) {
  return JSON.stringify(canonicalPreferenceScope(scope));
}

export function preferenceFamilyKey(scope: ViewPreferenceScope) {
  const base = canonicalPreferenceScope(scope);
  delete base.widgetSlot;

  return JSON.stringify(base);
}

export function isPreferenceScope(value: unknown): value is ViewPreferenceScope {
  return (
    isJsonObject(value) &&
    isString(value.viewId) &&
    !!value.viewId &&
    isViewContext(value.context) &&
    (value.slot === undefined || (isString(value.slot) && !!value.slot)) &&
    (value.widgetSlot === undefined || (isString(value.widgetSlot) && !!value.widgetSlot))
  );
}

// JSON numbers must survive a round trip without becoming null.
export function isPreferenceValue(value: unknown): value is JsonValue {
  if (!isJsonValue(value)) return false;

  if (typeof value === "number") return Number.isFinite(value);

  if (Array.isArray(value)) return value.every(isPreferenceValue);

  return !isJsonObject(value) || Object.values(value).every(isPreferenceValue);
}

export function isPreferenceResponse(value: unknown): value is ViewPreferenceResponse {
  return (
    isJsonObject(value) &&
    isPreferenceScope(value.scope) &&
    isFiniteNumber(value.revision) &&
    Number.isSafeInteger(value.revision) &&
    value.revision >= 0 &&
    isJsonObject(value.values) &&
    Object.values(value.values).every(isPreferenceValue) &&
    typeof value.migrationClosed === "boolean"
  );
}
