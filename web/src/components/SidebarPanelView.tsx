// A custom panel view in the Notes sidebar's slot (SPEC-0136 R6). The panel
// runs in its experience's panel frame, which the shared frame store creates
// on first use and keeps through subject changes (one page, tab `panel`), and
// suspends while the slot shows another panel, is minimised, or the sidebar is
// hidden. The frame lives in a layer of its own over the slot's body, because
// moving an iframe reloads it.
import type { ViewCatalogEntry } from "../api/types";
import { PANEL_TAB_ID } from "../lib/customViewMessages";
import type { BridgeServices } from "../views/viewBridge";
import type { PanelContext } from "./activePanel";
import { ExperiencePlaceholder, FrameLayer } from "./ExperienceFrameLayer";
import type { ExperienceFrames } from "./experienceFrames";
import { bridgePanelEscape, usePanelOpen } from "./SidebarPanelSlot";
import { bridgeSidebarShortcut } from "./WorkbenchSidebar";

/** What a panel's page may ask the workspace to do, as an experience page may. */
export type PanelServices = Omit<BridgeServices, "session" | "readLifecycle">;

const bridgePanelKeys = (frame: HTMLIFrameElement) => {
  bridgeSidebarShortcut(frame);
  bridgePanelEscape(frame);
};

/** The sidebar's panel frames, kept while their panel is away so each resumes as it was. */
export function SidebarPanelFrames({ frames }: { frames: ExperienceFrames }) {
  return <FrameLayer frames={frames} panel onFrameLoad={bridgePanelKeys} />;
}

export function SidebarPanelView({
  frames,
  view,
  experienceKey,
  context,
  services,
  active,
}: {
  frames: ExperienceFrames;
  view: ViewCatalogEntry;
  experienceKey: string;
  context: PanelContext;
  services: PanelServices;
  /** False while the sidebar is hidden. */
  active: boolean;
}) {
  const open = usePanelOpen();

  // Unmounting the placeholder suspends the frame.
  if (!open || !active) return null;

  return (
    <ExperiencePlaceholder
      layer={{ frames, tabId: PANEL_TAB_ID }}
      panel
      experienceKey={experienceKey}
      view={view}
      context={context}
      services={services}
    />
  );
}
