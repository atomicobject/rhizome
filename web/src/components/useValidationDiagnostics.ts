import { useInfiniteQuery } from "@tanstack/react-query";
import { useMemo } from "react";

import { getPublicValidationDiagnostics } from "../api/publicClient";
import { queryKeys } from "../api/queryKeys";
import type { ValidationDiagnostic, ValidationDiagnosticsParams } from "../api/types";

export type DiagnosticFilters = {
  check: string;
  code: string;
  /** Variant key within `code`. */
  variant?: string;
  text?: string;
  scopeKind?: "file" | "note" | "node" | "type" | "interface";
  scopeKey?: string;
  /** Applicable repairs are safe or confirmable; agent-only guidance is not. */
  repairAvailability: "any" | "applicable" | "inapplicable";
  /** File order keeps each file's issues together and reports per-file totals. */
  sort?: "diagnostic_order" | "file";
};

/** One page request for `filters`; callers add the cursor. */
export function diagnosticsParams(
  generation: number,
  filters: DiagnosticFilters,
  limit = 100,
): ValidationDiagnosticsParams {
  const params: ValidationDiagnosticsParams = {
    generation,
    limit,
    sort: filters.sort ?? "diagnostic_order",
  };

  if (filters.check) params.check = filters.check;

  if (filters.code) params.code = filters.code;

  if (filters.code && filters.variant) params.variant = filters.variant;

  if (filters.text) params.text = filters.text;

  if (filters.scopeKind && filters.scopeKey) {
    params.scopeKind = filters.scopeKind;
    params.scopeKey = filters.scopeKey;
  }

  if (filters.repairAvailability !== "any") {
    params.repairAvailability = filters.repairAvailability;
  }

  return params;
}

export function useValidationDiagnostics(
  generation: number | null,
  filters: DiagnosticFilters,
  enabled = true,
) {
  const query = useInfiniteQuery({
    queryKey: queryKeys.validationDiagnostics(generation ?? 0, filters),
    queryFn: ({ pageParam, signal }) =>
      getPublicValidationDiagnostics(
        { ...diagnosticsParams(generation ?? 0, filters), cursor: pageParam || undefined },
        { signal },
      ),
    initialPageParam: "",
    getNextPageParam: (page) => page.nextCursor,
    enabled: enabled && generation !== null && generation > 0,
  });

  const diagnostics: ValidationDiagnostic[] =
    query.data?.pages.flatMap((page) => page.diagnostics) ?? [];

  const total = query.data?.pages[0]?.total ?? 0;

  const fileTotals = useMemo(
    () => new Map(query.data?.pages.flatMap((page) => Object.entries(page.fileTotals ?? {})) ?? []),
    [query.data],
  );

  return { ...query, diagnostics, total, fileTotals };
}
