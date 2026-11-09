import type { PublicTypedRootCoverage } from "../src/api/publicGraphQLTypes";

/** How completely one typed root was read; see `typedRootCoverage`. */
export type TypedRootCoverage = PublicTypedRootCoverage;

type Coverage = Readonly<Record<string, TypedRootCoverage>>;

// Typed-root coverage, by the data object it arrived with.
const coverageByData = new WeakMap<WeakKey, Coverage>();

// GraphQL data is always an object.
function isData<T>(value: T): value is T & WeakKey {
  return typeof value === "object" && value !== null;
}

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
  return isData(data) ? coverageByData.get(data) : undefined;
}

/** Attach coverage to the data it describes, or clear stale coverage. */
export function recordCoverage<TData>(data: TData, coverage: Coverage | undefined) {
  if (!isData(data)) return;

  if (coverage) coverageByData.set(data, coverage);
  else coverageByData.delete(data);
}
