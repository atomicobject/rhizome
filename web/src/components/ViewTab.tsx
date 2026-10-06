import { isJsonObject } from "../api/parse";
import { isConfiguredViewVariant } from "./ConfiguredViewModel";
import { useEffect, useMemo } from "react";
import { ViewHost, type ViewServices } from "../views/ViewHost";
import { ViewSelector } from "../views/ViewSelector";
import { useViewSelection } from "../views/useViewSelection";
import type { ViewChoice, ViewTarget } from "../api/types";
import type { ViewTab as ViewTabModel } from "./useNoteTabs";
import { useViewCatalogQuery } from "./useNotesQueries";
import { useTypeLabel, viewDisplayName } from "./typeLabels";

type Props = {
  tab: ViewTabModel;
  active: boolean;
  editSession: Pick<
    ViewServices,
    "session" | "readLifecycle" | "vaultKey" | "busy" | "unacknowledged"
  >;
  onOpenNote: ViewServices["onOpenNote"];
  onOpenNode?: ViewServices["onOpenNode"];
  onOpenView?: ViewServices["onOpenView"];
  onStageOps: ViewServices["onStageOps"];
  onOpenIssues: NonNullable<ViewServices["onOpenIssues"]>;
  onSelectCollection?: ViewServices["onSelectCollection"];
  onOpenSearch?: ViewServices["onOpenSearch"];
  onTitle: (id: string, title: string) => void;
};

export function ViewTab({
  tab,
  active,
  editSession,
  onOpenNote,
  onOpenNode,
  onOpenView,
  onStageOps,
  onOpenIssues,
  onSelectCollection,
  onOpenSearch,
  onTitle,
}: Props) {
  const catalog = useViewCatalogQuery(active);
  const entry = catalog.data?.views.find((view) => view.id === tab.viewId) ?? null;

  const target = catalog.data?.targets?.find(
    (target) => target.kind === "standalone" && target.name === tab.viewId,
  );

  const fallback: ViewTarget = useMemo(() => {
    const variant = entry?.defaults?.variant ?? "";

    const choice: ViewChoice = {
      id: tab.viewId,
      name: entry?.name ?? tab.title,
      viewId: tab.viewId,
      renderer: entry?.source.kind === "custom" ? "custom" : "table",
      variant: isConfiguredViewVariant(variant) ? variant : undefined,
    };

    return { kind: "standalone", name: tab.viewId, choices: [choice], defaultChoiceId: choice.id };
  }, [entry, tab.title, tab.viewId]);

  const selected = useViewSelection({
    target: target ?? fallback,
    views: catalog.data?.views,
    vaultKey: editSession.vaultKey ?? null,
  });

  const typeLabel = useTypeLabel();
  const name = entry ? viewDisplayName(entry, typeLabel) : null;
  useEffect(() => {
    if (name && name !== tab.title) onTitle(tab.id, name);
  }, [name, tab.id, tab.title, onTitle]);

  if (!entry)
    return catalog.data ? (
      <p className="ontology-empty">
        This view is no longer available. Close this tab to continue.
      </p>
    ) : null;
  const choice = selected.choice ?? fallback.choices[0];
  const viewTarget = target ?? fallback;

  const host = (embedded: boolean) => (
    <ViewHost
      view={{ id: choice.viewId ?? choice.id, name: choice.name }}
      choice={choice}
      definition={entry}
      configuration={isJsonObject(entry.configuration) ? entry.configuration : undefined}
      context={{ kind: "standalone" }}
      active={active}
      services={{
        ...editSession,
        onOpenNote,
        onOpenNode,
        onOpenView,
        onStageOps,
        onOpenIssues,
        onSelectCollection,
        onOpenSearch,
      }}
      embedded={embedded}
    />
  );

  if (viewTarget.choices.length < 2) return host(false);

  const sourceType = entry.source.type || entry.source.interface;

  return (
    <>
      <header className="configured-view__header">
        <div className="configured-view__identity">
          {!entry.generated && sourceType && (
            <p className="configured-view__eyebrow">{typeLabel(sourceType)}</p>
          )}
          <h2>{name}</h2>
        </div>
        <ViewSelector
          target={viewTarget}
          selectedId={choice.id}
          onSelect={selected.select}
          status={selected}
        />
      </header>
      {selected.loading ? <p role="status">Loading view…</p> : host(true)}
    </>
  );
}
