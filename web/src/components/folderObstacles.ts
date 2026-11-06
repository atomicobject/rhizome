export type Obstacle = { id: string; x: number; y: number; size: number };

type Point = { x: number; y: number };

export class FolderObstacles {
  private cells = new Map<string, Obstacle[]>();
  readonly maxSize: number;
  constructor(
    points: Obstacle[],
    private cellSize = 0.025,
  ) {
    this.maxSize = Math.max(0, ...points.map((point) => point.size));

    for (const point of points) {
      const key = this.key(point.x, point.y);
      const cell = this.cells.get(key) ?? [];
      cell.push(point);
      this.cells.set(key, cell);
    }
  }
  private key(x: number, y: number) {
    return `${Math.floor(x / this.cellSize)},${Math.floor(y / this.cellSize)}`;
  }
  *nearRoute(a: Point, b: Point, padding: number): Generator<Obstacle> {
    const steps = Math.max(1, Math.ceil(Math.hypot(b.x - a.x, b.y - a.y) / this.cellSize));
    const reach = Math.ceil(padding / this.cellSize) + 1;
    const visited = new Set<string>();

    for (let step = 0; step <= steps; step++) {
      const x = Math.floor((a.x + ((b.x - a.x) * step) / steps) / this.cellSize);
      const y = Math.floor((a.y + ((b.y - a.y) * step) / steps) / this.cellSize);

      for (let dx = -reach; dx <= reach; dx++)
        for (let dy = -reach; dy <= reach; dy++) {
          const key = `${x + dx},${y + dy}`;

          if (visited.has(key)) continue;
          visited.add(key);
          yield* this.cells.get(key) ?? [];
        }
    }
  }
}
