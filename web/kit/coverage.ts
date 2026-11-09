// What a GraphQL response's `extensions` said about its data: typed-root
// coverage, truncated capped reads, and the index generation it was read at.
// Each is kept by the data object it arrived with.
import { replaceEqualDeep } from "@tanstack/react-query";

import { isString } from "../src/api/parse";
import type {
  PublicGraphQLExtensions,
  PublicTruncation,
  PublicTypedRootCoverage,
} from "../src/api/publicGraphQLTypes";

/** How completely one typed root was read; see `typedRootCoverage`. */
export type TypedRootCoverage = PublicTypedRootCoverage;

/** A capped read that stopped at its limit; see `truncation`. */
export type Truncation = PublicTruncation;

type Coverage = Readonly<Record<string, TypedRootCoverage>>;

type ResponseFacts = {
  coverage?: Coverage;
  truncated?: readonly Truncation[];
  generation?: string;
};

const factsByData = new WeakMap<WeakKey, ResponseFacts>();

// GraphQL data is always an object.
function isData<T>(value: T): value is T & WeakKey {
  return typeof value === "object" && value !== null;
}

const factsOf = <TData>(data: TData) => (isData(data) ? factsByData.get(data) : undefined);

/**
 * Typed-root coverage for data that `graphql` or `useGraphQL` returned, keyed
 * by response alias: whether each generated type root (such as
 * `assertion(first: 5000)`) returned every match. Status `complete` means it
 * did; any other status means the array is not the whole set and must be
 * disclosed. Undefined for data the kit did not read or a query with no typed
 * root. Nested relation lists are not covered: declared relation fields without
 * `first` return every row.
 */
export function typedRootCoverage<TData>(data: TData): Coverage | undefined {
  return factsOf(data)?.coverage;
}

/**
 * The capped reads in data that `graphql` or `useGraphQL` returned that
 * stopped at their limit: `nodes(refs:)`, `search`, and `neighborhood`, each
 * as `{ path, returned, limit }`. Empty when nothing was cut off.
 */
export function truncation<TData>(data: TData): readonly Truncation[] {
  return factsOf(data)?.truncated ?? [];
}

/** The index generation a kit read was answered at, when the server sent one. */
export function responseGeneration<TData>(data: TData): string | undefined {
  return factsOf(data)?.generation;
}

/** Attach a response's extensions to the data they describe, replacing older facts. */
export function recordResponse<TData>(
  data: TData,
  extensions: PublicGraphQLExtensions | undefined,
) {
  if (!isData(data)) return;

  const facts: ResponseFacts = {};

  if (extensions?.typedRoots) facts.coverage = extensions.typedRoots;

  if (extensions?.truncated?.length) facts.truncated = extensions.truncated;

  if (isString(extensions?.generation)) facts.generation = extensions.generation;

  factsByData.set(data, facts);
}

/**
 * TanStack's structural sharing, but an equal previous data object takes the
 * newer response's facts, so its coverage and generation stay true. Every kit
 * query client uses it.
 */
export function shareResponse<TData>(previous: TData | undefined, next: TData): TData {
  const shared = replaceEqualDeep(previous, next);

  if (shared !== next && isData(shared)) {
    const facts = factsOf(next);

    if (facts) factsByData.set(shared, facts);
    else factsByData.delete(shared);
  }

  return shared;
}
