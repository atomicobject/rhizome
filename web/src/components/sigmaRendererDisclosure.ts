import { drawDiscNodeLabel, type NodeHoverDrawingFunction } from "sigma/rendering";
import type { ThemeColors } from "./sigmaRenderer";
import type Sigma from "sigma";
import type { ExplorerNodeAttrs } from "./explorerGraph";
import type { SigmaEdgeAttrs } from "../lib/graphStyle";
import { resolveCssColor } from "../lib/themeColors";

/** The disclosure uses the same 24px chevron target as the legend. */
export function drawDisclosure(
  sigma: Sigma<ExplorerNodeAttrs, SigmaEdgeAttrs>,
  context: CanvasRenderingContext2D,
  id: string,
) {
  const node = sigma.getNodeDisplayData(id);

  if (!node) return;
  const { x, y } = sigma.framedGraphToViewport(node);
  context.strokeStyle = resolveCssColor("var(--explorer-link-strong)");
  context.lineWidth = 1.75;
  context.beginPath();
  context.moveTo(x - 4, y - 2);
  context.lineTo(x, y + 2);
  context.lineTo(x + 4, y - 2);
  context.stroke();
}

// WHY: Sigma's default hover box is hard-coded white, which hides the light
// label text of the dark theme. This draws the same shape on the surface token.
export function themedNodeHover(
  colors: () => ThemeColors,
): NodeHoverDrawingFunction<ExplorerNodeAttrs, SigmaEdgeAttrs> {
  return (context, data, settings) => {
    const size = settings.labelSize;
    const padding = 2;
    context.font = `${settings.labelWeight} ${size}px ${settings.labelFont}`;
    context.fillStyle = colors().surface;
    context.shadowBlur = 8;
    context.shadowColor = "rgba(0, 0, 0, 0.35)";

    const radius = Math.max(data.size, size / 2) + padding;
    context.beginPath();

    if (data.label) {
      const boxWidth = Math.round(context.measureText(data.label).width + 5);
      const boxHeight = Math.round(size + 2 * padding);
      const angle = Math.asin(boxHeight / 2 / radius);
      const dx = Math.sqrt(Math.abs(radius ** 2 - (boxHeight / 2) ** 2));
      context.moveTo(data.x + dx, data.y + boxHeight / 2);
      context.lineTo(data.x + radius + boxWidth, data.y + boxHeight / 2);
      context.lineTo(data.x + radius + boxWidth, data.y - boxHeight / 2);
      context.lineTo(data.x + dx, data.y - boxHeight / 2);
      context.arc(data.x, data.y, radius, angle, -angle);
    } else {
      context.arc(data.x, data.y, data.size + padding, 0, Math.PI * 2);
    }

    context.closePath();
    context.fill();
    context.shadowBlur = 0;
    drawDiscNodeLabel(context, data, settings);
  };
}
