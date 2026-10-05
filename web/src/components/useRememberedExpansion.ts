import { useState } from "react";

import { useScopedViewPreference } from "../viewPreferences/hooks";
import { NATIVE_EXPANSION_KEYS, validNativePreference } from "../viewPreferences/native";
import type { ViewPreferenceScope } from "../viewPreferences/types";

export { NATIVE_EXPANSION_KEYS } from "../viewPreferences/native";

/** Explicit expansion choices: grouping identity → group key → expanded. */
export type ExpansionChoices = Record<string, Record<string, boolean>>;

const MAX_GROUPINGS = 8;

const MAX_CHOICES = 400;

// Four renderers share an instance's 128 KiB of stored preferences with its layout.
const MAX_BYTES = 16 * 1024;

const encoder = new TextEncoder();

const jsonBytes = (value: string) => encoder.encode(JSON.stringify(value)).length;

/** The newest choices whose JSON fits the byte budget; long group keys drop older ones. */
function withinBudget(choices: ExpansionChoices): ExpansionChoices {
  let size = 2;
  const kept: [string, Record<string, boolean>][] = [];

  for (const [identity, items] of Object.entries(choices).reverse()) {
    // `"identity":{}` plus a separator, then `"key":false` plus a separator per choice.
    const header = jsonBytes(identity) + 4;
    const entries: [string, boolean][] = [];
    let used = header;

    for (const [key, expanded] of Object.entries(items).reverse()) {
      const entry = jsonBytes(key) + 7;

      if (size + used + entry > MAX_BYTES) continue;
      used += entry;
      entries.unshift([key, expanded]);
    }

    if (entries.length === 0) continue;
    size += used;
    kept.unshift([identity, Object.fromEntries(entries)]);
  }

  return Object.fromEntries(kept);
}

function isExpansionChoices(value: unknown): value is ExpansionChoices {
  return validNativePreference("tableGroups", value);
}

const NONE: ExpansionChoices = {};

const DEFINITION = { defaultValue: NONE, validate: isExpansionChoices };

const NO_SCOPE: ViewPreferenceScope = { viewId: "unscoped", context: { kind: "standalone" } };

/** Records one choice, moving its grouping to the end so older groupings drop first. */
export function withExpansionChoice(
  choices: ExpansionChoices,
  identity: string,
  key: string,
  expanded: boolean,
): ExpansionChoices {
  const { [identity]: current = {}, ...others } = choices;
  const { [key]: _previous, ...kept } = current;

  const grouping = Object.fromEntries(
    [...Object.entries(kept), [key, expanded]].slice(-MAX_CHOICES),
  );

  return withinBudget(
    Object.fromEntries(
      [...Object.entries(others), [identity, grouping] as const].slice(-MAX_GROUPINGS),
    ),
  );
}

/**
 * Remembered group expansion for one renderer of a view instance (SPEC-0114).
 * Choices belong to a grouping identity, such as the grouped fields and bucket,
 * so another grouping never inherits them, and stable group keys keep them
 * across refreshes, edits, filtering, and loading more rows. Without a scope
 * or vault the choices last as long as the mount.
 */
export function useRememberedExpansion(
  scope: ViewPreferenceScope | null | undefined,
  vaultKey: string | null | undefined,
  key: (typeof NATIVE_EXPANSION_KEYS)[number],
  identity: string,
) {
  const stored = useScopedViewPreference(
    scope ?? NO_SCOPE,
    scope ? (vaultKey ?? null) : null,
    key,
    DEFINITION,
  );

  const [local, setLocal] = useState<ExpansionChoices>({});
  const durable = Boolean(scope) && vaultKey !== null && vaultKey !== undefined;
  const choices = (durable ? stored.value : local)[identity] ?? {};

  const isExpanded = (groupKey: string, expandedByDefault: boolean) =>
    choices[groupKey] ?? expandedByDefault;

  const setExpanded = (groupKey: string, expanded: boolean) => {
    const next = (current: ExpansionChoices) =>
      withExpansionChoice(current, identity, groupKey, expanded);

    if (durable) void stored.update(next).catch(() => {});
    else setLocal(next);
  };

  return {
    choices,
    isExpanded,
    setExpanded,
    toggle: (groupKey: string, expandedByDefault: boolean) =>
      setExpanded(groupKey, !isExpanded(groupKey, expandedByDefault)),
  };
}
