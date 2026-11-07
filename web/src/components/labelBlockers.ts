type Box = [number, number, number, number];

type Blocker = { id: string; box: Box };

/** Index discs and placed labels by viewport cell; query with the label clearance. */
export class LabelBlockers {
  private cells = new Map<string, Blocker[]>();
  checks = 0;

  constructor(blockers: Blocker[]) {
    for (const blocker of blockers) this.add(blocker);
  }

  private *keys(box: Box, gap = 0) {
    for (let y = Math.floor((box[1] - gap) / 64); y <= Math.floor((box[3] + gap) / 64); y++) {
      for (let x = Math.floor((box[0] - gap) / 64); x <= Math.floor((box[2] + gap) / 64); x++)
        yield `${x}:${y}`;
    }
  }

  add(blocker: Blocker) {
    for (const key of this.keys(blocker.box)) {
      const cell = this.cells.get(key) ?? [];
      cell.push(blocker);
      this.cells.set(key, cell);
    }
  }

  overlaps(box: Box, exclude: string) {
    for (const key of this.keys(box, 8)) {
      for (const blocker of this.cells.get(key) ?? []) {
        this.checks++;
        const b = blocker.box;

        if (
          blocker.id !== exclude &&
          box[0] < b[2] + 8 &&
          box[2] + 8 > b[0] &&
          box[1] < b[3] + 8 &&
          box[3] + 8 > b[1]
        )
          return true;
      }
    }

    return false;
  }
}
