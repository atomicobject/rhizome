import { useViewPreference } from "@rhizome/kit";

import { isBoolean, isJsonObject } from "./api.ts";

type Choices = Record<string, Record<string, boolean>>;

const MAX_IDENTITIES = 8;

const MAX_CHOICES = 400;

// Trace and Briefing share an instance's 128 KiB of stored preferences.
const MAX_BYTES = 16 * 1024;

const encoder = new TextEncoder();

const jsonBytes = (value: string) => encoder.encode(JSON.stringify(value)).length;

/** The newest choices whose JSON fits the byte budget; long item keys drop older ones. */
function withinBudget(choices: Choices): Choices {
  let size = 2;
  const kept: [string, Record<string, boolean>][] = [];

  for (const [identity, items] of Object.entries(choices).reverse()) {
    // `"identity":{}` plus a separator, then `"key":false` plus a separator per choice.
    const header = jsonBytes(identity) + 4;
    const entries: [string, boolean][] = [];
    let used = header;

    for (const [key, open] of Object.entries(items).reverse()) {
      const entry = jsonBytes(key) + 7;

      if (size + used + entry > MAX_BYTES) continue;
      used += entry;
      entries.unshift([key, open]);
    }

    if (entries.length === 0) continue;
    size += used;
    kept.unshift([identity, Object.fromEntries(entries)]);
  }

  return Object.fromEntries(kept);
}

const isChoices = (value: unknown): value is Choices =>
  isJsonObject(value) &&
  Object.values(value).every(
    (items) => isJsonObject(items) && Object.values(items).every(isBoolean),
  );

const NONE: Choices = {};

const CHOICES = { defaultValue: NONE, validate: isChoices };

export function withChoice(
  choices: Choices,
  identity: string,
  key: string,
  open: boolean,
): Choices {
  const { [identity]: current = {}, ...others } = choices;
  const { [key]: _previous, ...kept } = current;

  const items = Object.fromEntries([...Object.entries(kept), [key, open]].slice(-MAX_CHOICES));

  return withinBudget(
    Object.fromEntries(
      [...Object.entries(others), [identity, items] as const].slice(-MAX_IDENTITIES),
    ),
  );
}

/** Open or closed choices for the items of one identity. */
export function useRememberedOpen(key: string, identity = "") {
  const preference = useViewPreference(key, CHOICES);
  const choices = preference.value[identity] ?? {};

  const setOpen = (item: string, open: boolean) =>
    void preference.update((current) => withChoice(current, identity, item, open)).catch(() => {});

  return {
    choices,
    isOpen: (item: string, openByDefault: boolean) => choices[item] ?? openByDefault,
    setOpen,
  };
}
