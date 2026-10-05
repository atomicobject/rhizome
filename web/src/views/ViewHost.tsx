import type { ComponentType } from "react";
import type {
  NodeRef,
  OntologyEditOp,
  OntologyEditSessionResponse,
  ValidationScope,
  ViewCatalogEntry,
  ViewChoice,
} from "../api/types";
import type { OpenMode } from "../components/useNoteTabs";
import type { EditReadLifecycle } from "../staging/stagedQuery";
import { CustomViewFrame } from "../components/CustomViewFrame";
import { ViewPreferencesStatus } from "../components/ViewPreferencesStatus";
import type { ViewPreferenceScope } from "../viewPreferences/types";
import { NativeCollectionView } from "./NativeCollectionView";
import type { OpenNodeOptions, ViewContext, ViewModuleProps } from "./context";

export type ViewServices = {
  session: OntologyEditSessionResponse | null;
  readLifecycle?: EditReadLifecycle;
  vaultKey?: string | null;
  busy?: boolean;
  unacknowledged?: boolean;
  onSelectCollection?: (typeName: string) => void;
  onOpenNote: (path: string, mode?: OpenMode) => void;
  onOpenNode?: (ref: NodeRef, options: OpenNodeOptions) => void;
  onOpenView?: (id: string, context: ViewContext) => void;
  onStageOps: (ops: OntologyEditOp[]) => Promise<void>;
  onOpenIssues?: (scope?: ValidationScope) => void;
};

export type ViewRuntimeProps = ViewModuleProps & {
  choice: ViewChoice;
  definition?: ViewCatalogEntry | null;
  active: boolean;
  services: ViewServices;
  embedded?: boolean;
};

/**
 * The preference instance of a custom view or built-in presentation: the
 * definition for a custom view, else the choice, such as `builtin:overview`.
 */
export function hostPreferenceScope({
  choice,
  definition,
  context,
  preferenceSlot,
}: Pick<
  ViewRuntimeProps,
  "choice" | "definition" | "context" | "preferenceSlot"
>): ViewPreferenceScope {
  const scope: ViewPreferenceScope = {
    viewId: definition?.id ?? choice.viewId ?? choice.id,
    context,
  };

  if (preferenceSlot) scope.slot = preferenceSlot;

  return scope;
}

function PreferencesBar(props: ViewRuntimeProps) {
  return (
    <div className="view-preferences-bar">
      <ViewPreferencesStatus
        scope={hostPreferenceScope(props)}
        vaultKey={props.services.vaultKey}
        includeWidgets={props.choice.renderer === "custom"}
      />
    </div>
  );
}

export function ViewHost(
  props: ViewRuntimeProps & {
    renderers?: Partial<Record<"overview" | "read" | "source", ComponentType<ViewRuntimeProps>>>;
  },
) {
  const { choice, definition, context, active, services, renderers } = props;

  if (choice.renderer === "custom") {
    if (!definition)
      return <p role="alert">This view is unavailable. Choose another view to continue.</p>;

    return active ? (
      <>
        <PreferencesBar {...props} />
        <CustomViewFrame
          view={definition}
          context={context}
          preferenceSlot={props.preferenceSlot}
          session={services.session}
          readLifecycle={services.readLifecycle}
          onOpenNote={services.onOpenNote}
          onOpenNode={services.onOpenNode}
          onOpenView={services.onOpenView}
          onStageOps={services.onStageOps}
          onOpenIssues={services.onOpenIssues}
          onSelectCollection={services.onSelectCollection}
        />
      </>
    ) : null;
  }

  if (choice.renderer === "table" || choice.renderer === "card" || choice.renderer === "kanban") {
    return definition ? (
      <NativeCollectionView {...props} definition={definition} />
    ) : (
      <p role="alert">This view is unavailable. Choose another view to continue.</p>
    );
  }

  const Renderer = renderers?.[choice.renderer];

  return Renderer ? (
    <>
      {choice.renderer === "overview" && <PreferencesBar {...props} />}
      <Renderer {...props} />
    </>
  ) : (
    <p role="alert">This presentation is unavailable. Choose another view to continue.</p>
  );
}
