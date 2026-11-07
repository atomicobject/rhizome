import {run} from './runtime.mjs';
import {sourceType} from './render.mjs';

export async function verifyIndex(root, ir, query = text => JSON.parse(run(process.env.RZM || 'rzm', ['ontology', 'query', '--query', text], {cwd: root}))) {
  const expected = new Map(ir.sources.map(source => [sourceType(source.kind, source.compositionOrigin), new Map()]));
  for (const record of ir.records) {
    const source = ir.sources.find(s => s.kind === record.kind && s.path === record.path);
    if (!source) throw new Error(`ao_index_source_missing: ${record.path}`);
    const counts = expected.get(sourceType(record.kind, source.compositionOrigin));
    counts.set(record.path, (counts.get(record.path) || 0) + 1);
  }
  let pending = [...expected].map(([type, counts], index) => ({type, counts, alias: `model${index}`, offset: 0, seen: new Set(), actual: new Map()}));
  while (pending.length) {
    const text = `{${pending.map(p => `${p.alias}: ${p.type[0].toLowerCase() + p.type.slice(1)}(first: 5000, offset: ${p.offset}) { nodeId notePath }`).join('\n')}}`;
    const response = await query(text);
    if (response.errors?.length) throw new Error(`ao_index_query_failed: ${response.errors[0].message}`);
    const next = [];
    for (const page of pending) {
      const rows = response.data?.[page.alias];
      const coverage = response.extensions?.typedRoots?.[page.alias];
      if (!Array.isArray(rows) || !coverage) throw new Error('ao_index_coverage_missing: typed-root coverage is required');
      for (const row of rows) {
        if (!row.nodeId || page.seen.has(row.nodeId)) throw new Error(`ao_index_duplicate: ${page.type}`);
        page.seen.add(row.nodeId);
        page.actual.set(row.notePath, (page.actual.get(row.notePath) || 0) + 1);
      }
      if (coverage.status === 'complete') {
        const sorted = counts => JSON.stringify([...counts].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0));
        if (sorted(page.actual) !== sorted(page.counts)) throw new Error(`ao_index_projection_incomplete: ${page.type} does not contain the current AO source inventory`);
      } else if (coverage.nextOffset > page.offset) {
        page.offset = coverage.nextOffset;
        next.push(page);
      } else throw new Error(`ao_index_coverage_incomplete: ${page.type} (${coverage.status})`);
    }
    pending = next;
  }
}
