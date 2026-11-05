import type { ExplorerNodeAttrs } from "./explorerGraph";
import type { EdgeReducerInput, EdgeReducerOutput } from "../lib/graphStyle";
import {
  colorWithPremultipliedAlpha,
  isTypeHighlighted,
  lerp,
  TYPE_HIGHLIGHT_ALPHA,
} from "../lib/graphStyle";
import { resolveCssColor } from "../lib/themeColors";

export function computeEdgeStyle(
  input: EdgeReducerInput & { sourceAttrs: ExplorerNodeAttrs; targetAttrs: ExplorerNodeAttrs },
): EdgeReducerOutput {
  const { source, target, attrs, sourceAttrs, targetAttrs, state, lod } = input;

  if (attrs.kind === "folder-child-crosslink" || attrs.kind === "folder-crosslink")
    return { hidden: true, color: resolveCssColor("var(--explorer-link)"), size: 1, label: "" };

  if (
    attrs.kind === "explorer-documents" ||
    attrs.kind === "explorer-code" ||
    attrs.kind === "explorer-tests"
  )
    return { hidden: true, color: resolveCssColor("var(--explorer-link)"), size: 1, label: "" };

  if (attrs.kind === "folder-containment")
    return {
      hidden: true,
      color: resolveCssColor("var(--type-8)"),
      size: 1.5,
      label: "",
    };

  const hasTypeHighlighting = state.highlightTypes.size > 0;

  const sourceTypeMatch =
    !hasTypeHighlighting || isTypeHighlighted(sourceAttrs.resolvedType, state.highlightTypes);

  const targetTypeMatch =
    !hasTypeHighlighting || isTypeHighlighted(targetAttrs.resolvedType, state.highlightTypes);

  const hasGraphMatches = state.graphMatches.size > 0;

  const filterActive =
    (state.searchActive && state.searchOnlyMatches) ||
    (state.graphFilter && hasGraphMatches) ||
    state.activeCommunity !== null;

  const sSemantic = state.semanticMatches.has(source);
  const tSemantic = state.semanticMatches.has(target);
  const sGraph = hasGraphMatches && state.graphMatches.has(source);
  const tGraph = hasGraphMatches && state.graphMatches.has(target);
  const sCluster = state.activeCommunity ? sourceAttrs.community === state.activeCommunity : true;
  const tCluster = state.activeCommunity ? targetAttrs.community === state.activeCommunity : true;

  if (filterActive) {
    let sAllowed = true;
    let tAllowed = true;

    if (state.searchActive && state.searchOnlyMatches) {
      sAllowed = sAllowed && sSemantic;
      tAllowed = tAllowed && tSemantic;
    }

    if (state.graphFilter && hasGraphMatches) {
      sAllowed = sAllowed && sGraph;
      tAllowed = tAllowed && tGraph;
    }

    if (state.activeCommunity) {
      sAllowed = sAllowed && sCluster;
      tAllowed = tAllowed && tCluster;
    }

    if (!sAllowed || !tAllowed) {
      return { hidden: true, color: state.colors.edgeFaint, size: 0.5, label: "" };
    }
  }

  const isHoverEdge =
    state.hoverNodeID !== null && (source === state.hoverNodeID || target === state.hoverNodeID);

  const isFocus =
    sSemantic ||
    tSemantic ||
    sGraph ||
    tGraph ||
    (state.activeCommunity && (sCluster || tCluster)) ||
    isHoverEdge;

  const hasHighlighting =
    (state.searchActive && state.semanticMatches.size > 0) ||
    hasGraphMatches ||
    state.activeCommunity !== null;

  const sizeScale = lerp(1.15, 0.82, lod);
  const size = Math.max(0.35, attrs.size * sizeScale);
  // Edge names show only for what the reader is looking at: a hovered node's or a match's links.
  const labelVisible = state.showEdgeLabels && Boolean(isFocus);
  let label = labelVisible ? attrs.label : "";

  let color: string;

  const { edgeFaint, edgeLight, edgeStrong } = state.colors;

  if (isFocus || isHoverEdge) {
    color = edgeStrong;
  } else if (hasHighlighting || state.hoverNodeID !== null) {
    color = edgeFaint;
  } else {
    color = lod > 0.3 ? edgeFaint : edgeLight;
  }

  if (hasTypeHighlighting && (!sourceTypeMatch || !targetTypeMatch) && !isFocus) {
    color = colorWithPremultipliedAlpha(edgeFaint, TYPE_HIGHLIGHT_ALPHA.edge);
    label = "";
  }

  // The folder overview draws few links, so each is essential: opaque in the
  // 3:1 neutral at 1–2px, fading only when the reader points at another folder.
  // Minor folder links (folderGraph's MINOR_FOLDER_LINK) wait for a hover on one of their folders.
  if (attrs.kind === "folder-minor" && !isFocus)
    return { hidden: true, color: edgeFaint, size: 0.5, label: "" };

  if (attrs.kind === "folder-containment" || attrs.kind === "folder-minor") {
    const faded = (hasHighlighting || state.hoverNodeID !== null) && !isFocus;

    return {
      hidden: false,
      color: faded ? edgeFaint : colorWithPremultipliedAlpha(state.colors.neutral, 0.35),
      size: 1,
      label: attrs.kind === "folder-containment" ? "" : label,
    };
  }

  return {
    hidden: false,
    color,
    size,
    label,
  };
}
