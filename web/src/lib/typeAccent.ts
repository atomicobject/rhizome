import { useOntologySummaryQuery } from "../components/useNotesQueries";
import { appQueryClient } from "../api/queryClient";
import { queryKeys } from "../api/queryKeys";
import type { OntologySummaryResponse } from "../api/types";
import { typeColor, typeSlot } from "./typePalette";

export { NEUTRAL_TYPE_COLOR } from "./typePalette";

// The palette rule lives in typePalette.ts, which the view kit re-exports, so
// the app and bundled views give a type the same hue.
const summary = () =>
  appQueryClient.getQueryData<OntologySummaryResponse>(queryKeys.ontology.summary());

/** A type's palette slot, the same on every surface; a name the ontology does not list falls back to a hash. */
export function typeAccentIndex(name: string): number {
  return typeSlot(summary(), name);
}

/** A type's color as a CSS value. It marks dots, borders, and edges, never text. Interfaces take the neutral. */
export function typeAccentColor(name: string): string {
  return typeColor(summary(), name);
}

/**
 * The summary that type colors follow. A surface that memoizes colors lists it
 * as a dependency, so colors settle once the summary arrives.
 */
export function useTypeAccentKey() {
  return useOntologySummaryQuery().data;
}
