// Which sidebar panel the active tab shows (SPEC-0136 R6). The server resolves
// a panel descriptor for every view target; the host only picks the active
// tab's target and follows `{ kind: owner }` to the owning note's target.
import type { NodeWorkspace, OntologySummaryResponse, ViewCatalog, ViewPanel } from "../api/types";
import { publicTypeName } from "../lib/typeNames";
import type { ViewContext } from "../views/context";
import type { NotesLocation } from "./notesRoute";
import type { Tab } from "./useNoteTabs";

/** A panel the host can show: never `owner`, which it has already followed. */
export type ResolvedPanel = Exclude<ViewPanel, { kind: "owner" }>;

/**
 * The active subject's context, as the panel's invocation takes it. A
 * standalone view names itself, so the panel route can find its experience.
 */
export type PanelContext = ViewContext | { kind: "standalone"; view: string };

export type ActivePanel = { context: PanelContext; panel: ResolvedPanel };

const FOLDER: ResolvedPanel = { kind: "folder" };

const NONE: ResolvedPanel = { kind: "none" };

type Inputs = {
  tab: Tab;
  catalog: ViewCatalog | undefined;
  homeSelection: NotesLocation["selection"];
  /** The home tab's selected type or interface, when one is selected. */
  homeType: string | null;
  summary: OntologySummaryResponse | null;
  /** The active note tab's node, once it has loaded. */
  workspace: NodeWorkspace | null | undefined;
};

function targetPanel(catalog: ViewCatalog | undefined, kind: string, name: string) {
  return catalog?.targets?.find((target) => target.kind === kind && target.name === name)?.panel;
}

function resolved(panel: ViewPanel | undefined, fallback: ResolvedPanel): ResolvedPanel {
  return !panel || panel.kind === "owner" ? fallback : panel;
}

function nodePanel(catalog: ViewCatalog | undefined, workspace: NodeWorkspace): ResolvedPanel {
  const panel = targetPanel(catalog, "node", publicTypeName(workspace.node.resolvedType));

  // A subject without a target is still backed by a note.
  if (panel?.kind !== "owner") return panel ?? FOLDER;

  // An owner descriptor on a note itself means Folder; on an embedded or
  // section node it means the panel of the note that holds it.
  if (workspace.node.ref.kind.toUpperCase() === "NOTE") return FOLDER;

  return resolved(targetPanel(catalog, "node", publicTypeName(workspace.node.noteType)), FOLDER);
}

/** The active tab's panel and the context its invocation takes; null while unknown. */
export function activePanel({
  tab,
  catalog,
  homeSelection,
  homeType,
  summary,
  workspace,
}: Inputs): ActivePanel | null {
  if (!catalog) return null;

  switch (tab.kind) {
    case "home": {
      if (homeSelection.kind === "group") {
        const { group } = homeSelection;

        return {
          context: { kind: "group", group },
          panel: resolved(targetPanel(catalog, "group", group), NONE),
        };
      }

      if (homeSelection.kind === "all")
        return {
          context: { kind: "workspace" },
          panel: resolved(targetPanel(catalog, "workspace", ""), NONE),
        };

      if (homeSelection.kind !== "type" || !homeType) return null;
      const isInterface = summary?.interfaces?.some((each) => each.name === homeType);

      return {
        context: isInterface
          ? { kind: "interface", interface: homeType }
          : { kind: "type", type: homeType },
        panel: resolved(targetPanel(catalog, isInterface ? "interface" : "type", homeType), NONE),
      };
    }

    case "view":
      return {
        context: { kind: "standalone", view: tab.viewId },
        panel: resolved(targetPanel(catalog, "standalone", tab.viewId), NONE),
      };

    case "note":
      return workspace
        ? {
            context: {
              kind: "node",
              type: publicTypeName(workspace.node.resolvedType),
              ref: workspace.node.ref,
            },
            panel: nodePanel(catalog, workspace),
          }
        : null;

    case "collection":
    case "search":
      return { context: { kind: "workspace" }, panel: NONE };

    default: {
      const exhaustive: never = tab;

      return exhaustive;
    }
  }
}
