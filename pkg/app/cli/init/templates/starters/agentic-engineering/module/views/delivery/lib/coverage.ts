import type { Coverage } from './model.ts';

export function readComplete(read: Coverage): boolean {
  return read.complete && !read.truncated && read.errors.length === 0
    && read.graphGeneration !== null;
}

export function isComplete(read: Coverage): boolean {
  return readComplete(read) && read.sourceRevision !== null;
}

/** Combine only reads of the same source. Different sources have different revisions. */
export function composeCoverage(reads: readonly Coverage[]): Coverage {
  const first = reads[0];
  const errors = reads.flatMap(read => read.errors);
  if (!first) return { complete: false, errors: ['No read evidence'], truncated: false,
    sourceRevision: null, graphGeneration: null };
  if (reads.some(read => read.sourceRevision !== first.sourceRevision)) errors.push('Source revision changed');
  if (reads.some(read => read.graphGeneration !== first.graphGeneration)) errors.push('Graph generation changed');
  return { ...first, complete: reads.every(isComplete) && errors.length === 0,
    truncated: reads.some(read => read.truncated), errors };
}

export type OccurrencePage<T> = Coverage & {
  offset: number;
  first: number;
  count: number;
  items: readonly T[];
};

export function composePages<T>(pages: readonly OccurrencePage<T>[]): { items: T[]; coverage: Coverage } {
  const coverage = composeCoverage(pages);
  const items: T[] = [];
  const count = pages[0]?.count;
  for (const page of pages) {
    if (page.offset !== items.length || page.count !== count || page.count < 0) {
      coverage.errors = [...coverage.errors, 'Inconsistent occurrence paging'];
      break;
    }
    const expected = Math.min(Math.max(0, Math.min(100, page.first)), page.count - page.offset);
    if (page.items.length !== expected) {
      coverage.errors = [...coverage.errors, 'Unexpected source-link page length'];
    }
    items.push(...page.items);
  }
  if (items.length !== count) coverage.errors = [...coverage.errors, 'Occurrence traversal incomplete'];
  coverage.complete = coverage.complete && coverage.errors.length === 0;
  return { items, coverage };
}

/** Inventory roots and hydration can span sources, but must share graph generation. */
export function inventoryComplete(reads: readonly Coverage[]): boolean {
  return reads.length > 0 && reads.every(readComplete)
    && reads.every(read => read.graphGeneration === reads[0].graphGeneration);
}
