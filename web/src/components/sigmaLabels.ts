// Explorer node labels: placed clear of node discs, other labels, and (where
// possible) edges, then drawn on a solid backing in the canvas color, pinned to the
// node, so a label never reads as belonging to a line or a neighbor.
//
// WHY: Sigma draws labels in label-grid order, not by size, and its grid
// ignores forced (landmark) labels. For a graph small enough to check, every
// label that will be drawn is placed once per frame, largest node first, before
// Sigma asks for the first one; Sigma's draw call then only paints the chosen
// spot. Larger graphs place labels in Sigma's order against labels alone.
import type Sigma from "sigma";
import type { NodeLabelDrawingFunction } from "sigma/rendering";
import type { SigmaEdgeAttrs, SigmaNodeAttrs } from "../lib/graphStyle";

type Box = [left: number, top: number, right: number, bottom: number];

type Spot = { x: number; y: number; box: Box };

type LabelColors = { label: string; canvas: string };

/** Up to this many nodes, labels are placed clear of node discs and edges as well as other labels. */
export const NODE_AVOID_ORDER = 10000;

/** Padding of the label's backing around its text. */
const PAD_X = 4;

const PAD_Y = 2;

/** A planned label's key: its node's center on screen, which the draw call receives too. */
const at = (x: number, y: number) => `${Math.round(x)}:${Math.round(y)}`;

const overlaps = (a: Box, b: Box) =>
  a[0] < b[2] + 8 && a[2] + 8 > b[0] && a[1] < b[3] + 8 && a[3] + 8 > b[1];

/** Whether the segment from (x1, y1) to (x2, y2) passes through the box (Liang–Barsky). */
function crosses(box: Box, x1: number, y1: number, x2: number, y2: number) {
  let t0 = 0;
  let t1 = 1;
  const dx = x2 - x1;
  const dy = y2 - y1;

  for (const [p, q] of [
    [-dx, x1 - box[0]],
    [dx, box[2] - x1],
    [-dy, y1 - box[1]],
    [dy, box[3] - y1],
  ]) {
    if (p === 0) {
      if (q < 0) return false;
      continue;
    }

    const t = q / p;

    if (p < 0) t0 = Math.max(t0, t);
    else t1 = Math.min(t1, t);

    if (t0 > t1) return false;
  }

  return true;
}

/**
 * The spots a label may take, each touching its node: right and left on the
 * node's middle, below and above, then hugging the four corners. `x, y` is
 * where the text starts and its baseline.
 */
function spotsAround(x: number, y: number, r: number, width: number, size: number): Spot[] {
  const gap = r + 12;
  const middle = y + size * 0.35;
  const corner = r * 0.75;

  const at = (sx: number, sy: number): Spot => ({
    x: sx,
    y: sy,
    box: [sx - PAD_X, sy - size - PAD_Y + 1, sx + width + PAD_X, sy + PAD_Y + 3],
  });

  return [0, 16, 32, 48, 64, 80, 96].flatMap((offset) => [
    at(x + gap + offset, middle),
    at(x - gap - offset - width, middle),
    at(x - width / 2, y + gap + offset + size - 2),
    at(x - width / 2, y - gap - offset - 2),
    at(x + gap + offset - 2, y - corner - offset - 2),
    at(x + gap + offset - 2, y + corner + offset + size),
    at(x - gap - offset + 2 - width, y - corner - offset - 2),
    at(x - gap - offset + 2 - width, y + corner + offset + size),
  ]);
}

/**
 * Builds Sigma's label drawing function. `beginFrame` must be called from
 * Sigma's "beforeRender" so each frame places its labels afresh.
 */
export function createLabelPainter(
  getSigma: () => Sigma<SigmaNodeAttrs, SigmaEdgeAttrs> | null,
  colors: () => LabelColors,
) {
  let planned: Map<string, Spot | null> | null = null;
  const placed: Box[] = [];

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
    const discs: { id: string; box: Box }[] = [];
    const centers = new Map<string, { x: number; y: number }>();
    const labeled: { id: string; label: string; x: number; y: number; r: number }[] = [];

    graph.forEachNode((id) => {
      const data = sigma.getNodeDisplayData(id);

      if (!data || data.hidden) return;
      const { x, y } = sigma.framedGraphToViewport(data);
      const r = sigma.scaleSize(data.size);
      centers.set(id, { x, y });
      discs.push({ id, box: [x - r - 2, y - r - 2, x + r + 2, y + r + 2] });

      if (data.label && (data.forceLabel || r >= threshold))
        labeled.push({ id, label: data.label, x, y, r });
    });

    const segments: [number, number, number, number][] = [];

    graph.forEachEdge((edge, _attrs, source, target) => {
      const display = sigma.getEdgeDisplayData(edge);
      const a = centers.get(source);
      const b = centers.get(target);

      if (display && !display.hidden && a && b) segments.push([a.x, a.y, b.x, b.y]);
    });

    labeled.sort((a, b) => b.r - a.r || a.id.localeCompare(b.id));

    for (const node of labeled) {
      const width = context.measureText(node.label).width;

      const candidates = spotsAround(node.x, node.y, node.r, width, size).filter(
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
        const count = nearby.filter(([x1, y1, x2, y2]) => crosses(spot.box, x1, y1, x2, y2)).length;

        if (count < fewest) {
          chosen = spot;
          fewest = count;
        }
      }

      if (chosen) placed.push(chosen.box);
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
    const size = settings.labelSize;
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

    if (distance > data.size + 4) {
      context.strokeStyle = label;
      context.lineWidth = 1;
      context.beginPath();
      context.moveTo(
        data.x + ((endX - data.x) * data.size) / distance,
        data.y + ((endY - data.y) * data.size) / distance,
      );
      context.lineTo(endX, endY);
      context.stroke();
    }

    context.fillStyle = canvas;
    context.fillRect(left, top, right - left, bottom - top);
    context.fillStyle = label;
    context.fillText(data.label, spot.x, spot.y);
  };

  return { draw, beginFrame };
}
