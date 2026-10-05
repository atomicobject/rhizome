import { BaseEdge, type EdgeProps } from "@xyflow/react";

import { isFiniteNumber, isJsonObject } from "../api/parse";

type RoutedPoint = { x: number; y: number };

function isRoutedPoint(value: unknown): value is RoutedPoint {
  return isJsonObject(value) && isFiniteNumber(value.x) && isFiniteNumber(value.y);
}

function isRoutedPointList(value: unknown): value is RoutedPoint[] {
  return Array.isArray(value) && value.every(isRoutedPoint);
}

function normalizedRoutePoints(
  points: RoutedPoint[],
  source: RoutedPoint,
  target: RoutedPoint,
): RoutedPoint[] {
  if (points.length < 4) {
    const middleX = (source.x + target.x) / 2;

    return [source, { x: middleX, y: source.y }, { x: middleX, y: target.y }, target];
  }

  const bends = points.slice(1, -1).map((point) => ({ ...point }));
  bends[0].y = source.y;
  bends[bends.length - 1].y = target.y;

  return [source, ...bends, target];
}

function edgePath(points: RoutedPoint[]): string {
  if (points.length === 0) return "";
  const [start, ...rest] = points;

  return [`M ${start.x} ${start.y}`, ...rest.map((point) => `L ${point.x} ${point.y}`)].join(" ");
}

function midpoint(points: RoutedPoint[]): { x: number; y: number } | undefined {
  if (points.length === 0) return undefined;

  if (points.length === 1) return points[0];

  let totalLength = 0;

  const segments = points.slice(1).map((point, index) => {
    const start = points[index];
    const length = Math.hypot(point.x - start.x, point.y - start.y);
    totalLength += length;

    return { start, end: point, length };
  });

  let remaining = totalLength / 2;

  for (const segment of segments) {
    if (remaining <= segment.length) {
      const ratio = segment.length === 0 ? 0 : remaining / segment.length;

      return {
        x: segment.start.x + (segment.end.x - segment.start.x) * ratio,
        y: segment.start.y + (segment.end.y - segment.start.y) * ratio,
      };
    }

    remaining -= segment.length;
  }

  return points.at(-1);
}

export function OntologyRoutedEdge({
  id,
  data,
  label,
  labelStyle,
  labelBgStyle,
  labelBgPadding,
  labelBgBorderRadius,
  markerEnd,
  markerStart,
  style,
  sourceX,
  sourceY,
  targetX,
  targetY,
  interactionWidth,
}: EdgeProps) {
  const points = data?.routedPoints;

  const routedPoints = normalizedRoutePoints(
    isRoutedPointList(points) ? points : [],
    { x: sourceX, y: sourceY },
    { x: targetX, y: targetY },
  );

  const path = edgePath(routedPoints);
  const labelPoint = midpoint(routedPoints);

  return (
    <BaseEdge
      id={id}
      path={path}
      label={label}
      labelX={labelPoint?.x}
      labelY={labelPoint?.y}
      labelStyle={labelStyle}
      labelBgStyle={labelBgStyle}
      labelBgPadding={labelBgPadding}
      labelBgBorderRadius={labelBgBorderRadius}
      markerEnd={markerEnd}
      markerStart={markerStart}
      style={style}
      interactionWidth={interactionWidth}
    />
  );
}
