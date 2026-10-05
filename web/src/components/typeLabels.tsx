import { createContext, type ReactNode, useCallback, useContext, useMemo } from "react";

import type { ViewCatalogEntry } from "../api/types";
import { humanizeName } from "../lib/labels";
import { publicTypeName } from "../lib/typeNames";
import { useOntologySummaryQuery } from "./useNotesQueries";

type TypeLabel = { label?: string; pluralLabel?: string };

const TypeLabelsContext = createContext<ReadonlyMap<string, TypeLabel>>(new Map());

/** Schema labels for types and interfaces, read by `useTypeLabel`. */
export function TypeLabelsProvider({ children }: { children: ReactNode }) {
  const summary = useOntologySummaryQuery().data;

  const labels = useMemo(
    () =>
      new Map<string, TypeLabel>(
        [...(summary?.types ?? []), ...(summary?.interfaces ?? [])].map((item) => [
          item.name,
          item,
        ]),
      ),
    [summary],
  );

  return <TypeLabelsContext.Provider value={labels}>{children}</TypeLabelsContext.Provider>;
}

export type TypeLabelFn = (name: string | null | undefined, plural?: boolean) => string;

/**
 * Reader-facing name for an ontology type: the schema label when the summary
 * has one, otherwise the humanized type name ("IPAsset" → "IP asset").
 */
export function useTypeLabel(): TypeLabelFn {
  const labels = useContext(TypeLabelsContext);

  return useCallback(
    (name, plural = false) => {
      const publicName = publicTypeName(name);
      const entry = labels.get(publicName);

      return (plural ? entry?.pluralLabel : undefined) || entry?.label || humanizeName(publicName);
    },
    [labels],
  );
}

/** Generated views carry the raw type name; show the type's label instead. */
export function viewDisplayName(view: ViewCatalogEntry, typeLabel: TypeLabelFn): string {
  if (!view.generated) return view.name;

  return typeLabel(view.source.type || view.source.interface || view.name, true);
}
