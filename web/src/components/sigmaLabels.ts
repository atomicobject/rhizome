import { LabelBlockers } from "./labelBlockers";
import { resolveCssColor } from "../lib/themeColors";
import { noteLabelLines, selectedNoteSpot } from "./sigmaLabelText";
import type { ExplorerNodeAttrs as SigmaNodeAttrs } from "./explorerGraph";
import type Sigma from "sigma";
import type { NodeLabelDrawingFunction } from "sigma/rendering";
import type { SigmaEdgeAttrs } from "../lib/graphStyle";

type Box = [left: number, top: number, right: number, bottom: number];

type Spot = { x: number; y: number; box: Box; detail?: boolean };

type LabelColors = { label: string; canvas: string; edgeLabel?: string };

export const NODE_AVOID_ORDER = 10000;

const PAD_X = 4;

const PAD_Y = 2;

const at = (x: number, y: number) => `${Math.round(x)}:${Math.round(y)}`;

const overlaps = (a: Box, b: Box, gap = 8) =>
  a[0] < b[2] + gap && a[2] + gap > b[0] && a[1] < b[3] + gap && a[3] + gap > b[1];

/** Whether the segment from (x1, y1) to (x2, y2) passes through the box (Liang–Barsky). */
function crosses(box: Box, x1: number, y1: number, x2: number, y2: number) {
  const dx = x2 - x1;
  const dy = y2 - y1;

  if (dx === 0 && (x1 < box[0] || x1 > box[2])) return false;

  if (dy === 0 && (y1 < box[1] || y1 > box[3])) return false;
  const tx1 = dx === 0 ? -Infinity : (box[0] - x1) / dx;
  const tx2 = dx === 0 ? Infinity : (box[2] - x1) / dx;
  const ty1 = dy === 0 ? -Infinity : (box[1] - y1) / dy;
  const ty2 = dy === 0 ? Infinity : (box[3] - y1) / dy;

  return (
    Math.max(0, Math.min(tx1, tx2), Math.min(ty1, ty2)) <=
    Math.min(1, Math.max(tx1, tx2), Math.max(ty1, ty2))
  );
}

/**
 * The spots a label may take, each touching its node: right and left on the
 * node's middle, below and above, then hugging the four corners. `x, y` is
 * where the text starts and its baseline.
 */
function spotsAround(
  x: number,
  y: number,
  r: number,
  width: number,
  size: number,
  left = false,
  landmark = false,
): Spot[] {
  const gap = r + 12;
  const middle = y + size * 0.35;
  const corner = r * 0.75;

  const at = (sx: number, sy: number): Spot => ({
    x: sx,
    y: sy,
    box: [sx - PAD_X, sy - size - PAD_Y + 1, sx + width + PAD_X, sy + PAD_Y + 3],
  });

  return (
    landmark ? [0, 16, 32, 48, 64, 80, 96, 128, 160, 192, 224, 256] : [0, 16, 32, 48, 64, 80, 96]
  ).flatMap((offset) => [
    at(left ? x - gap - offset - width : x + gap + offset, middle),
    at(left ? x + gap + offset : x - gap - offset - width, middle),
    at(x - width / 2, y + gap + offset + size - 2),
    at(x - width / 2, y - gap - offset - 2),
    at(x + gap + offset - 2, y - corner - offset - 2),
    at(x + gap + offset - 2, y + corner + offset + size),
    at(x - gap - offset + 2 - width, y - corner - offset - 2),
    at(x - gap - offset + 2 - width, y + corner + offset + size),
  ]);
}

/** Fixed Explorer labels stay on the outward side, within a short leader of the disc. */
export function outwardSpots(
  x: number,
  y: number,
  r: number,
  width: number,
  size: number,
  dx: number,
  dy: number,
): Spot[] {
  const length = Math.hypot(dx, dy) || 1;
  const side = dx / length;

  return [0, 4, -4].map((nudge) => {
    const sx = side >= 0.35 ? x + r + 6 : side <= -0.35 ? x - r - 6 - width : x - width / 2 + nudge;

    const sy =
      Math.abs(side) >= 0.35 ? y + size * 0.35 + nudge : dy < 0 ? y - r - 6 : y + r + 6 + size;

    return { x: sx, y: sy, box: [sx - PAD_X, sy - size, sx + width + PAD_X, sy + 4] satisfies Box };
  });
}

export function createLabelPainter(
  getSigma: () => Sigma<SigmaNodeAttrs, SigmaEdgeAttrs> | null,
  colors: () => LabelColors,
) {
  let planned: Map<string, Spot | null> | null = null;
  const placed: Box[] = [];
  const widths = new Map<string, number>();

  const measure = (context: CanvasRenderingContext2D, text: string) => {
    const key = `${context.font}:${text}`;
    const cached = widths.get(key);

    if (cached !== undefined) return cached;
    const width = context.measureText(text).width;
    widths.set(key, width);

    return width;
  };

  const beginFrame = () => {
    planned = null;
    placed.length = 0;
  };

  /** Place every label this frame will draw, largest node first, clear of discs, labels, and edges. */
  const plan = (context: CanvasRenderingContext2D, size: number, threshold: number) => {
    const sigma = getSigma();
    const spots = new Map<string, Spot | null>();

    if (!sigma) return spots;
    const graph = sigma.getGraph();
    const { width: viewWidth, height: viewHeight } = sigma.getDimensions();
    const discs: { id: string; box: Box }[] = [{ id: "graph-controls", box: [0, 0, 44, 100] }];
    const centers = new Map<string, { x: number; y: number }>();

    const labeled: {
      id: string;
      label: string;
      x: number;
      y: number;
      r: number;
      detail: string;
      left: boolean;
      priority: number;
      center: boolean;
      landmark: boolean;
      fixed: boolean;
      dx: number;
      dy: number;
    }[] = [];

    graph.forEachNode((id) => {
      const data = sigma.getNodeDisplayData(id);

      if (!data || data.hidden) return;
      const { x, y } = sigma.framedGraphToViewport(data);
      const r = sigma.scaleSize(data.size);
      centers.set(id, { x, y });
      discs.push({ id, box: [x - r - 2, y - r - 2, x + r + 2, y + r + 2] });

      const attrs = graph.getNodeAttributes(id);

      const parentPath = attrs.aggregate
        ? attrs.path || ""
        : (attrs.path || "").split("/").slice(0, -1).join("/");

      const parent = sigma.getNodeDisplayData(`folder:${parentPath}`);
      const origin = parent ? sigma.framedGraphToViewport(parent) : { x, y: y - 1 };

      if (data.label && (data.forceLabel || r >= threshold))
        labeled.push({
          id,
          label: data.label,
          x,
          y,
          r,
          detail: attrs.labelDetail || "",
          left: Boolean(attrs.fixed && attrs.x < 0),
          priority: attrs.priority ?? 0,
          center: Boolean(attrs.explorerLabels && attrs.center),
          landmark: Boolean(attrs.fixed && attrs.kind === "module"),
          fixed: Boolean(attrs.fixed),
          dx: id === "folder:" ? 0 : x - origin.x,
          dy: id === "folder:" ? 1 : y - origin.y,
        });
    });

    const segments: [number, number, number, number][] = [];

    graph.forEachEdge((edge, _attrs, source, target) => {
      const display = sigma.getEdgeDisplayData(edge);
      const a = centers.get(source);
      const b = centers.get(target);

      if (display && !display.hidden && a && b) segments.push([a.x, a.y, b.x, b.y]);
    });

    labeled.sort((a, b) => b.priority - a.priority || b.r - a.r || a.id.localeCompare(b.id));

    const blockerIndex = new LabelBlockers(discs);

    const addPlaced = (box: Box) => {
      placed.push(box);
      blockerIndex.add({ id: "", box });
    };

    const font = context.font;

    for (const node of labeled) {
      context.font = node.center
        ? `600 ${size}px Inter, -apple-system, system-ui, sans-serif`
        : font;

      const attrs = graph.getNodeAttributes(node.id);

      const lines =
        attrs.kind === "note" || attrs.kind === "embedded"
          ? noteLabelLines(node.label, (text) => measure(context, text), attrs.selected ? 140 : 220)
          : [node.label];

      node.label = lines.reduce(
        (longest, line) => (measure(context, line) > measure(context, longest) ? line : longest),
        "",
      );

      if (
        (attrs.selected && attrs.path && !(attrs.fixed && attrs.kind === "module")) ||
        (attrs.explorerLabels && attrs.fixed && attrs.kind !== "module")
      ) {
        const width =
          measure(context, node.label) + (node.detail ? 6 + measure(context, node.detail) : 0);

        const chosen =
          attrs.selected && attrs.kind === "note"
            ? selectedNoteSpot(node.x, node.y, node.r, width, size)
            : outwardSpots(
                node.x,
                node.y,
                node.r,
                width,
                size,
                attrs.kind === "note" || attrs.kind === "embedded" ? -1 : 1,
                0,
              )[0];

        chosen.box[3] += (lines.length - 1) * (size + 3);
        addPlaced(chosen.box);
        spots.set(at(node.x, node.y), chosen);
        continue;
      }

      if (node.fixed) {
        const detail = Boolean(node.detail);
        const width = Math.max(measure(context, node.label), measure(context, node.detail));

        function* candidates() {
          yield* outwardSpots(node.x, node.y, node.r, width, size, node.dx, node.dy);
          yield* spotsAround(node.x, node.y, node.r, width, size, node.left, true);

          // Dense fans may need a free label position elsewhere in the viewport.
          for (let y = size + PAD_Y; y < viewHeight - size - 8; y += size + 12) {
            for (let x = PAD_X; x + width + PAD_X <= viewWidth; x += 16) {
              yield {
                x,
                y,
                box: [
                  x - PAD_X,
                  y - size - PAD_Y + 1,
                  x + width + PAD_X,
                  y + PAD_Y + 3,
                ] satisfies Box,
              };
            }
          }
        }

        let chosen: Spot | undefined;
        let first: Spot | undefined;

        for (const spot of candidates()) {
          if (detail && spot.box[3] < node.y) {
            spot.y -= size + 3;
            spot.box[1] -= size + 3;
          } else if (detail) spot.box[3] += size + 3;
          first ??= spot;

          if (
            spot.box[0] >= 0 &&
            spot.box[2] <= viewWidth &&
            spot.box[1] >= 0 &&
            spot.box[3] <= viewHeight &&
            !blockerIndex.overlaps(spot.box, node.id)
          ) {
            chosen = spot;
            break;
          }
        }

        chosen ??= first!;

        chosen.detail = detail;
        addPlaced(chosen.box);
        spots.set(at(node.x, node.y), chosen);
        continue;
      }

      const width =
        measure(context, node.label) + (node.detail ? 6 + measure(context, node.detail) : 0);

      const candidates = spotsAround(
        node.x,
        node.y,
        node.r,
        width,
        size,
        node.left,
        node.landmark,
      ).filter(
        ({ box }) => box[0] >= 0 && box[2] <= viewWidth && box[1] >= 0 && box[3] <= viewHeight,
      );

      if (!candidates.length) {
        spots.set(at(node.x, node.y), null);
        continue;
      }

      // Only discs, labels and edges near this node's candidate spots can collide with them.
      const region: Box = [
        Math.min(...candidates.map(({ box }) => box[0])),
        Math.min(...candidates.map(({ box }) => box[1])),
        Math.max(...candidates.map(({ box }) => box[2])),
        Math.max(...candidates.map(({ box }) => box[3])),
      ];

      const blockers = [
        ...discs.flatMap((disc) =>
          disc.id === node.id || !overlaps(region, disc.box) ? [] : [disc.box],
        ),
        ...placed.filter((box) => overlaps(region, box)),
      ];

      const nearby = segments.filter(([x1, y1, x2, y2]) =>
        overlaps(region, [Math.min(x1, x2), Math.min(y1, y2), Math.max(x1, x2), Math.max(y1, y2)]),
      );

      // Clear spots first, then the one crossing the fewest edges, then the earliest side.
      let chosen: Spot | null = null;
      let fewest = Infinity;

      for (const spot of candidates) {
        if (blockers.some((other) => overlaps(other, spot.box))) continue;
        let count = 0;

        for (const [x1, y1, x2, y2] of nearby) {
          if (crosses(spot.box, x1, y1, x2, y2)) count++;

          if (count >= fewest) break;
        }

        if (count < fewest) {
          chosen = spot;
          fewest = count;

          if (count === 0) break;
        }
      }

      if (chosen) addPlaced(chosen.box);
      spots.set(at(node.x, node.y), chosen);
    }

    return spots;
  };

  const draw: NodeLabelDrawingFunction<SigmaNodeAttrs, SigmaEdgeAttrs> = (
    context,
    data,
    settings,
  ) => {
    if (!data.label) return;
    const attrs = getSigma()?.getGraph().getNodeAttributes(data.key);
    const size = attrs?.selected && attrs.kind === "code" ? 15 : settings.labelSize;
    context.font = `${settings.labelWeight} ${size}px ${settings.labelFont}`;
    const sigma = getSigma();
    const small = sigma ? sigma.getGraph().order <= NODE_AVOID_ORDER : false;

    let spot: Spot | null | undefined;

    if (small) {
      planned ??= plan(context, size, settings.labelRenderedSizeThreshold);
      spot = planned.get(at(data.x, data.y));
    }

    if (spot === undefined) {
      // Larger graphs: the first spot clear of the labels already drawn this frame.
      const width = context.measureText(data.label).width;

      const free = spotsAround(data.x, data.y, data.size, width, size).find(
        ({ box }) => !placed.some((other) => overlaps(other, box)),
      );

      spot = free ?? null;

      if (spot) placed.push(spot.box);
    }

    if (!spot) return;
    const [left, top, right, bottom] = spot.box;
    const { label, canvas } = colors();
    const endX = Math.max(left, Math.min(data.x, right));
    const endY = Math.max(top, Math.min(data.y, bottom));
    const distance = Math.hypot(endX - data.x, endY - data.y);

    if (
      !attrs?.selected &&
      distance > data.size + 4 &&
      (!attrs?.explorerLabels || attrs.kind === "module" || distance <= data.size + 24)
    ) {
      if (attrs?.explorerLabels) context.setLineDash([1, 3]);
      context.strokeStyle = attrs?.fixed ? colors().edgeLabel || label : label;
      context.globalAlpha = attrs?.fixed ? 0.4 : 1;
      context.lineWidth = 1;
      context.beginPath();
      context.moveTo(
        data.x + ((endX - data.x) * data.size) / distance,
        data.y + ((endY - data.y) * data.size) / distance,
      );
      context.lineTo(endX, endY);
      context.stroke();
      context.globalAlpha = 1;
      context.setLineDash([]);
    }

    context.fillStyle = canvas;

    context.fillRect(left, top, right - left, bottom - top);
    context.globalAlpha = 1;
    context.fillStyle = data.key.startsWith("file-more:")
      ? resolveCssColor("var(--explorer-link-strong)")
      : attrs?.muted
        ? colors().edgeLabel || label
        : label;
    context.font = `${attrs?.selected || data.key.startsWith("file-group:") ? "600" : settings.labelWeight} ${size}px ${settings.labelFont}`;

    const lines =
      attrs?.kind === "note" || attrs?.kind === "embedded"
        ? noteLabelLines(
            data.label,
            (text) => context.measureText(text).width,
            attrs?.selected ? 140 : 220,
          )
        : [data.label.replaceAll("...", "…")];

    lines.forEach((line, index) => {
      const prefix =
        attrs?.kind === "module" && line.includes("/ ")
          ? line.slice(0, line.lastIndexOf("/ ") + 2)
          : "";

      const labelX =
        attrs?.fixed && attrs.kind === "module" && spot.box[2] < data.x
          ? spot.box[2] - PAD_X - context.measureText(line).width
          : spot.x;

      if (prefix) {
        context.fillStyle = colors().edgeLabel || label;
        context.fillText(prefix, labelX, spot.y);
        context.fillStyle = attrs?.muted ? colors().edgeLabel || label : label;
      }

      context.fillText(
        line.slice(prefix.length),
        labelX + context.measureText(prefix).width,
        spot.y + index * (size + 3),
      );
    });

    if (attrs?.labelDetail && spot.detail !== false) {
      const stacked = attrs.fixed && attrs.kind === "module";

      const start = stacked
        ? spot.x
        : spot.x + Math.max(...lines.map((line) => context.measureText(line).width)) + 6;

      context.fillStyle = colors().edgeLabel || label;
      context.font = `500 ${Math.max(13, size)}px ${settings.labelFont}`;
      context.fillText(attrs.labelDetail, start, spot.y + (stacked ? size + 3 : 0));
    }

    context.globalAlpha = 1;
  };

  return { draw, beginFrame, occupied: () => placed };
}
