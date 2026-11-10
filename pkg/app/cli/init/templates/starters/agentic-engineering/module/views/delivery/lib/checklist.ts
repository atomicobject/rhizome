import type { Checklist } from './model.ts';

/** Only recorded Markdown task items count. This does not resolve links or scope. */
export function recordedChecklist(text: string | null | undefined, readable = true): Checklist {
  if (!readable) return { state: 'unreadable' };
  if (text === null || text === undefined) return { state: 'missing' };
  let fence: { marker: string; length: number } | null = null;
  let total = 0;
  let checked = 0;
  for (const line of text.split(/\r?\n/)) {
    const delimiter = /^[ \t]*(`{3,}|~{3,})(.*)$/.exec(line);
    if (delimiter) {
      const marker = delimiter[1];
      if (!fence) fence = { marker: marker[0], length: marker.length };
      else if (marker[0] === fence.marker && marker.length >= fence.length && !delimiter[2].trim()) fence = null;
      continue;
    }
    if (fence) continue;
    const task = /^\s*(?:[-+*]|\d+[.)])\s+\[([ xX])\](?:\s+|$)/.exec(line);
    if (!task) continue;
    total++;
    if (task[1].toLowerCase() === 'x') checked++;
  }
  return { state: total ? 'recorded' : 'empty', checked, total };
}
