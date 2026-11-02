// View parameters (SPEC-0110): declared values on a view's URL query. The
// host and the kit read and write them with these same rules.
import { isBoolean, isPlainObject, isString } from "../api/parse";

export type ViewParamKind = "string" | "int" | "bool";

export type ViewParamValue = string | number | boolean;

export type ViewParamDeclaration = {
  name: string;
  kind: ViewParamKind;
  default?: ViewParamValue;
  identity?: boolean;
};

/** Declared params by name; a param with no default and no value is absent. */
export type ViewParams = Record<string, ViewParamValue>;

/** A change to some params; `null` returns a param to its default. */
export type ViewParamsPatch = Record<string, ViewParamValue | null>;

const KINDS: readonly string[] = ["string", "int", "bool"];

function matchesKind(kind: ViewParamKind, value: unknown): value is ViewParamValue {
  if (kind === "string") return isString(value);

  if (kind === "bool") return isBoolean(value);

  return Number.isSafeInteger(value);
}

/* oxlint-disable anti-slop/no-unknown-parameters -- These parsers validate server and postMessage data. */
/** Server-sent declarations, dropping any entry that is not well formed. */
export function parseViewParamDeclarations(value: unknown): ViewParamDeclaration[] {
  if (!Array.isArray(value)) return [];

  return value.flatMap((entry): ViewParamDeclaration[] => {
    if (!isPlainObject(entry) || !isString(entry.name) || !KINDS.includes(String(entry.kind)))
      return [];
    // SAFETY: checked against KINDS above.
    const kind = entry.kind as ViewParamKind;
    const declaration: ViewParamDeclaration = { name: entry.name, kind };

    if (matchesKind(kind, entry.default)) declaration.default = entry.default;

    if (entry.identity === true) declaration.identity = true;

    return [declaration];
  });
}

/** One query value read as its kind; undefined when it does not parse. */
export function coerceViewParam(kind: ViewParamKind, raw: string): ViewParamValue | undefined {
  if (kind === "string") return raw;

  if (kind === "bool")
    return raw === "true" || raw === "1"
      ? true
      : raw === "false" || raw === "0"
        ? false
        : undefined;

  const value = /^-?\d+$/.test(raw) ? Number(raw) : NaN;

  return Number.isSafeInteger(value) ? value : undefined;
}

/** The declared params on a query, defaults applied; undeclared keys are ignored. */
export function readViewParams(
  declarations: readonly ViewParamDeclaration[],
  search: string | URLSearchParams,
) {
  const query = isString(search) ? new URLSearchParams(search) : search;

  return Object.fromEntries(
    declarations.flatMap(({ name, kind, default: fallback }) => {
      const raw = query.get(name);
      const value = (raw === null ? undefined : coerceViewParam(kind, raw)) ?? fallback;

      return value === undefined ? [] : [[name, value] as const];
    }),
  );
}

/**
 * Check a patch against the declarations. Returns the reason it is invalid, or
 * null when every key is declared and every value matches its kind.
 */
export function viewParamsPatchError(
  declarations: readonly ViewParamDeclaration[],
  patch: unknown,
): string | null {
  if (!isPlainObject(patch)) return "expected an object of params";

  for (const [name, value] of Object.entries(patch)) {
    const declaration = declarations.find((each) => each.name === name);

    if (!declaration) return `param ${JSON.stringify(name)} is not declared`;

    if (value !== null && !matchesKind(declaration.kind, value))
      return `param ${JSON.stringify(name)} must be a ${declaration.kind} value or null`;
  }

  return null;
}

/* oxlint-enable anti-slop/no-unknown-parameters */

/** Write a patch onto a query. A value equal to its default leaves the URL. */
export function applyViewParamsPatch(
  declarations: readonly ViewParamDeclaration[],
  query: URLSearchParams,
  patch: ViewParamsPatch,
) {
  for (const [name, value] of Object.entries(patch)) {
    const declaration = declarations.find((each) => each.name === name);

    if (value === null || value === declaration?.default) query.delete(name);
    else query.set(name, String(value));
  }
}

/** The identity params' values, compared to decide whether a change remounts. */
export function viewParamsIdentity(
  declarations: readonly ViewParamDeclaration[],
  params: ViewParams,
) {
  return JSON.stringify(
    declarations.flatMap((each) => (each.identity ? [params[each.name] ?? null] : [])),
  );
}

/** Add param values to an href's query, keeping its path and fragment. */
export function withViewParams(href: string, params?: Record<string, ViewParamValue>) {
  if (!params || Object.keys(params).length === 0) return href;
  const url = new URL(href, "http://rhizome.invalid");

  for (const [name, value] of Object.entries(params)) url.searchParams.set(name, String(value));

  return `${url.pathname}${url.search}${url.hash}`;
}

/** A flat object of string, safe-integer, or boolean values, as `openView` takes. */
export function isViewParamValues(value: unknown): value is Record<string, ViewParamValue> {
  return (
    isPlainObject(value) &&
    Object.values(value).every(
      (each) => isString(each) || isBoolean(each) || Number.isSafeInteger(each),
    )
  );
}
