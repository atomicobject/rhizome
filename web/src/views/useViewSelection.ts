import { decodeJson, isJsonObject, isString, isStringArray } from "../api/parse";
import { useCallback, useMemo, useState } from "react";
import { isViewContext, type ViewContext } from "./context";
import { useViewPreferences } from "../viewPreferences/hooks";
import {
  canonicalPreferenceScope,
  preferenceScopeKey,
  type PreferenceMigration,
  type ViewPreferenceScope,
} from "../viewPreferences/types";
import type { ViewCatalogEntry, ViewChoice, ViewTarget } from "../api/types";

export function viewSelectionKey(
  vaultKey: string | null,
  target: Pick<ViewTarget, "kind" | "name">,
  subject?: string,
) {
  return `rhizome:view:selection:v1:${JSON.stringify([vaultKey, target.kind, subject ?? target.name])}`;
}

export function resolveViewChoice(
  target: ViewTarget | null | undefined,
  explicit?: string | null,
  stored?: string | null,
  views?: ViewCatalogEntry[],
): ViewChoice | null {
  const choices = target?.choices ?? [];

  const find = (id: string | null | undefined) => {
    if (!id) return undefined;
    const exact = choices.find((choice) => choice.id === id);

    if (exact) return exact;
    const replaced = generatedChoiceVariant(id);

    // A replaceGenerated view took the generated layouts' slot.
    if (replaced) return choices.find((choice) => !choice.custom && choice.variant === replaced);
    const matches = choices.filter((choice) => choice.viewId === id);
    const variant = views?.find((view) => view.id === id)?.defaults?.variant;

    return (
      matches.find((choice) => choice.variant === variant) ??
      matches.find((choice) => choice.id === target?.defaultChoiceId) ??
      matches[0]
    );
  };

  return find(explicit) ?? find(stored) ?? find(target?.defaultChoiceId) ?? choices[0] ?? null;
}

/** The layout of a generated view's choice ID (`view:["generated.…","card"]`). */
function generatedChoiceVariant(id: string) {
  if (!id.startsWith("view:")) return undefined;
  const [viewId, variant] = decodeJson(id.slice(5), isStringArray) ?? [];

  return viewId?.startsWith("generated.") ? variant : undefined;
}

const LEGACY_MODES = "rhizome:notes:viewModeByType";

const NATIVE_RENDERERS = ["table", "card", "kanban"];

function legacyVariantKeys(vaultKey: string | null, viewId: string) {
  return [
    `rhizome:view:variant:v2:${encodeURIComponent(vaultKey ?? "")}:${viewId}`,
    `rhizome.view.variant.${viewId}`,
  ];
}

// Pre-SPEC-0110 preferences: a type's Home/Table mode and a native view's variant.
function legacySelection(target: ViewTarget, vaultKey: string | null) {
  const typed = target.kind === "type" || target.kind === "interface";

  const modes = typed
    ? (decodeJson(window.localStorage.getItem(LEGACY_MODES), isJsonObject) ?? {})
    : {};

  if (modes[target.name] === "home")
    return {
      id: target.choices.find((choice) => choice.renderer === "overview")?.id ?? null,
      source: `${LEGACY_MODES}:${vaultKey}:${target.kind}:${target.name}`,
      cleanup: null,
    };

  if (!typed && target.kind !== "standalone") return null;
  const native = target.choices.filter((choice) => NATIVE_RENDERERS.includes(choice.renderer));

  // Table mode opened the type's authored view, preferring its default, mounted as
  // default or not. Only without one does it mean the generated Table, now the default.
  const authoredChoices = native.filter((choice) => !choice.viewId?.startsWith("generated."));

  const authored =
    modes[target.name] === "table"
      ? (
          authoredChoices.find((choice) => choice.id === target.defaultChoiceId) ??
          authoredChoices[0]
        )?.viewId
      : undefined;

  const viewId =
    authored ??
    (native.find((choice) => choice.id === target.defaultChoiceId) ?? native[0])?.viewId;

  if (!viewId) return null;

  for (const key of legacyVariantKeys(vaultKey, viewId)) {
    const variant = window.localStorage.getItem(key);
    const choice = native.find((choice) => choice.viewId === viewId && choice.variant === variant);

    if (choice)
      return {
        id: choice.id,
        source: key.startsWith("rhizome:view:variant:v2:") ? key : `${key}:${vaultKey}`,
        cleanup: key.startsWith("rhizome:view:variant:v2:") ? key : null,
      };
  }

  // A registered view ID resolves to that view's declared default variant.
  return authored
    ? {
        id: authored,
        source: `${LEGACY_MODES}:${vaultKey}:${target.kind}:${target.name}`,
        cleanup: null,
      }
    : null;
}

function selectionScope(
  target: ViewTarget | null | undefined,
  context?: ViewContext,
): ViewPreferenceScope | null {
  if (!target) return null;

  const subject: ViewContext | null =
    context ??
    (target.kind === "type"
      ? { kind: "type", type: target.name }
      : target.kind === "interface"
        ? { kind: "interface", interface: target.name }
        : target.kind === "group"
          ? { kind: "group", group: target.name }
          : target.kind === "standalone" || target.kind === "workspace"
            ? { kind: target.kind }
            : null);

  if (!subject) return null;
  const scope = canonicalPreferenceScope({ viewId: "$selection", context: subject });

  if (!isViewContext(scope.context)) return null;

  if (
    (subject.kind === "type" && subject.type === "*") ||
    (subject.kind === "interface" && subject.interface === "*") ||
    (subject.kind === "node" && subject.type === "*") ||
    (subject.kind === "standalone" && !target.name.trim())
  )
    return null;

  if (subject.kind === "standalone") scope.slot = target.name;

  return scope;
}

function selectionMigration(
  target: ViewTarget | null | undefined,
  vaultKey: string | null,
  subject?: string,
  notePath?: string,
): PreferenceMigration[] {
  if (!target || vaultKey === null) return [];

  try {
    const key = viewSelectionKey(vaultKey, target, subject);
    const current = window.localStorage.getItem(key);
    const legacy = current === null ? legacySelection(target, vaultKey) : null;
    const id = current ?? legacy?.id;

    const migrations: PreferenceMigration[] = id
      ? [
          {
            migrationId: current !== null ? key : legacy!.source,
            values: id === "__default__" ? {} : { choice: id },
            acknowledged: () => {
              try {
                window.localStorage.removeItem(key);

                if (legacy?.cleanup) window.localStorage.removeItem(legacy.cleanup);
              } catch {}
            },
          },
        ]
      : [];

    if (notePath) {
      const noteKey = `rhizome:notes:view-mode:v1:${notePath}`;
      const mode = window.sessionStorage.getItem(noteKey);

      if (mode === "source" || mode === "read")
        migrations.push({
          migrationId: noteKey,
          values: current === null && mode === "source" ? { choice: "builtin:source" } : {},
          acknowledged: () => {
            try {
              window.sessionStorage.removeItem(noteKey);
            } catch {}
          },
        });
    }

    return migrations;
  } catch {
    return [];
  }
}

export function useViewSelection({
  target,
  views,
  explicit,
  vaultKey,
  subject,
  context,
  onSelect,
}: {
  target: ViewTarget | null | undefined;
  views?: ViewCatalogEntry[];
  explicit?: string | null;
  vaultKey: string | null;
  subject?: string;
  context?: ViewContext;
  onSelect?: (id: string | null) => void;
}) {
  const scope = selectionScope(target, context);

  const stateKey = JSON.stringify([
    vaultKey,
    scope ? preferenceScopeKey(scope) : [target?.kind, subject ?? target?.name],
  ]);

  const [temporary, setTemporary] = useState<Record<string, string | null>>({});

  const notePath =
    scope?.context.kind === "node" && scope.context.ref.kind === "NOTE"
      ? scope.context.ref.notePath
      : undefined;

  const migrations = useMemo(
    () => selectionMigration(target, vaultKey, subject, notePath),
    [target, vaultKey, subject, notePath],
  );

  const preferences = useViewPreferences(scope, vaultKey, migrations);
  const saved = preferences.values.choice;
  const stored = !preferences.store ? temporary[stateKey] : isString(saved) ? saved : null;
  const choice = resolveViewChoice(target, explicit, stored, views);

  const update = useCallback(
    (id: string | null) => {
      if (!preferences.store) setTemporary((current) => ({ ...current, [stateKey]: id }));
      else
        void preferences.store
          .patch(id === null ? { unset: ["choice"] } : { set: { choice: id } })
          .catch(() => {});
      onSelect?.(id);
    },
    [onSelect, preferences.store, stateKey],
  );

  return {
    choice,
    select: (id: string) => update(id === target?.defaultChoiceId ? null : id),
    loading: preferences.loading,
    pending: preferences.pending,
    error:
      preferences.error ??
      (Object.hasOwn(preferences.values, "choice") && !isString(saved)
        ? new Error("Invalid saved view choice")
        : null),
    reset: preferences.resetAll,
    retry: preferences.retry,
  };
}
