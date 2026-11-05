import type { OntologySummaryResponse } from "../api/types";

/**
 * The twelve type hues in tokens.css, in assignment order: consecutive slots
 * differ in hue and lightness, so types next to each other in the ontology
 * never look alike. Slot 8 is the neutral and is never a type's color.
 */
const TYPE_SLOTS = [1, 6, 7, 5, 2, 3, 4, 11, 10, 12, 13, 9];

/** The neutral slot: interfaces, code, and untyped notes. */
export const NEUTRAL_TYPE_COLOR = "var(--type-8)";

/** The parts of the ontology summary that type colors follow. */
export type TypeSummaryInput = Pick<OntologySummaryResponse, "types" | "interfaces"> | undefined;

type TypeOrder = { types: Map<string, number>; interfaces: Set<string> };

const orders = new WeakMap<object, TypeOrder>();

const EMPTY: TypeOrder = { types: new Map(), interfaces: new Set() };

/**
 * Concrete types in name order with the types that have records first. Name
 * order holds as counts change, and twelve hues cover twelve types before any
 * two share a color; a type only moves when it gains its first record.
 */
function typeOrder(summary: TypeSummaryInput): TypeOrder {
  if (!summary) return EMPTY;

  let order = orders.get(summary);

  if (order) return order;
  const types = summary.types ?? [];

  const named = (withRecords: boolean) =>
    types.flatMap((type) => (type.count > 0 === withRecords ? [type.name] : [])).sort();

  const names = [...new Set([...named(true), ...named(false)])];

  order = {
    types: new Map(names.map((name, index) => [name, index])),
    interfaces: new Set((summary.interfaces ?? []).map((item) => item.name)),
  };
  orders.set(summary, order);

  return order;
}

function hashName(name: string) {
  let hash = 0;

  for (let i = 0; i < name.length; i++) hash = (hash * 31 + name.charCodeAt(i)) >>> 0;

  return hash;
}

/** A type's palette slot, the same on every surface; a name the ontology does not list falls back to a hash. */
export function typeSlot(summary: TypeSummaryInput, name: string): number {
  const position = typeOrder(summary).types.get(name) ?? hashName(name);
  const lap = Math.floor(position / TYPE_SLOTS.length);

  // Past twelve types hues repeat, starting halfway down the list so the
  // thirteenth type does not match the first, its nearest neighbor by name.
  return TYPE_SLOTS[(position + lap * (TYPE_SLOTS.length / 2)) % TYPE_SLOTS.length];
}

/** A type's color as a CSS value. It marks dots, borders, and edges, never text. Interfaces take the neutral. */
export function typeColor(summary: TypeSummaryInput, name: string): string {
  if (typeOrder(summary).interfaces.has(name)) return NEUTRAL_TYPE_COLOR;

  return `var(--type-${typeSlot(summary, name)})`;
}
