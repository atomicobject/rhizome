/**
 * Boundary parsers and type guards.
 *
 * Every `unknown` that enters the UI (JSON.parse, res.json(), SSE `data`,
 * localStorage) is decoded here into a named type. Ordinary code branches on
 * the decoded value; it never runs `typeof` itself. Add a guard here only
 * when it decodes I/O shared by more than one module; a guard over a
 * module-private type (e.g. a stored preference) belongs next to that type.
 */
import type { components } from "./generated";
import type { NodeEvent, NodeRef } from "./types";

export type JsonPrimitive = string | number | boolean | null;

export type JsonValue = JsonPrimitive | JsonValue[] | JsonObject;

export type JsonObject = { [key: string]: JsonValue };

/** Wire error payload (`ErrorResponse` in the OpenAPI schema). */
export type ErrorResponse = components["schemas"]["ErrorResponse"];

export function isString(v: unknown): v is string {
  return typeof v === "string";
}

export function isFiniteNumber(v: unknown): v is number {
  return typeof v === "number" && Number.isFinite(v);
}

export function isBoolean(v: unknown): v is boolean {
  return typeof v === "boolean";
}

/** Capability probe for environments/fakes that may omit a method (`res.clone`, `crypto.randomUUID`). */
export function isCallable(v: unknown): v is Function {
  return typeof v === "function";
}

export function isJsonValue(v: unknown): v is JsonValue {
  if (v === null || typeof v === "string" || typeof v === "number" || typeof v === "boolean") {
    return true;
  }

  if (Array.isArray(v)) return v.every(isJsonValue);

  return isJsonObject(v);
}

/** Plain object whose values are all JSON; rejects arrays, class instances, and `null`. */
export function isJsonObject(v: unknown): v is JsonObject {
  if (typeof v !== "object" || v === null || Array.isArray(v)) return false;
  const proto = Object.getPrototypeOf(v);

  if (proto !== Object.prototype && proto !== null) return false;

  return Object.values(v).every(isJsonValue);
}

export function isStringArray(v: unknown): v is string[] {
  return Array.isArray(v) && v.every(isString);
}

export function isOptionalString(v: unknown): v is string | undefined {
  return v === undefined || typeof v === "string";
}

function isOptionalFiniteNumber(v: unknown): v is number | undefined {
  return v === undefined || isFiniteNumber(v);
}

/**
 * JSON text → guarded value. `null` for empty input, invalid JSON, or a value
 * the guard rejects. Use for localStorage/sessionStorage reads and SSE `data`.
 */
export function decodeJson<T>(
  raw: string | null | undefined,
  guard: (v: unknown) => v is T,
): T | null {
  if (!raw) return null;
  let parsed: unknown;

  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }

  return guard(parsed) ? parsed : null;
}

const NODE_REF_OPTIONAL_STRINGS = [
  "fragment",
  "nodeId",
  "typeName",
  "parentId",
  "structuralFingerprint",
] as const;

/** Also discriminates `string | NodeRef` targets: the false branch narrows to `string`. */
export function isNodeRef(v: unknown): v is NodeRef {
  return (
    isJsonObject(v) &&
    isString(v.notePath) &&
    isString(v.kind) &&
    NODE_REF_OPTIONAL_STRINGS.every((key) => isOptionalString(v[key])) &&
    isOptionalFiniteNumber(v.startByte) &&
    isOptionalFiniteNumber(v.endByte)
  );
}

/** Node SSE message body (`/api/v1/nodes/events`). */
export function isNodeEvent(v: unknown): v is NodeEvent {
  return (
    isJsonObject(v) &&
    isString(v.id) &&
    isString(v.kind) &&
    isNodeRef(v.ref) &&
    isOptionalString(v.version) &&
    isOptionalString(v.cause) &&
    (v.changed === undefined || isStringArray(v.changed))
  );
}

/** REST error body; `details` stays a JSON object rather than `unknown`. */
export function isErrorResponse(v: unknown): v is ErrorResponse {
  return (
    isJsonObject(v) &&
    isString(v.error) &&
    isOptionalString(v.code) &&
    (v.details === undefined || isJsonObject(v.details))
  );
}
