export type Box = [left: number, top: number, right: number, bottom: number];

export type Spot = { x: number; y: number; box: Box; detail?: boolean };

const PAD_X = 4;

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
