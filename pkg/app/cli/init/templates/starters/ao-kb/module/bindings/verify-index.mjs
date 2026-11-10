import {run, readJSON} from './runtime.mjs';
import path from 'node:path';
import {siteInventory} from './knowledge.mjs';
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

// A complete empty result is valid. Missing roots or pending coverage are not.
export async function verifyKnowledgeIndex(root, query = text => JSON.parse(run(process.env.RZM || 'rzm', ['ontology', 'query', '--query', text], {cwd: root}))) {
  const selection = await readJSON(path.join(root, '.rhizome/modules/ao-kb/bindings/selection.json'));
  const files = await siteInventory(root, selection?.product);
  for (let offset = 0; offset < files.length; offset += 100) {
    const batch = files.slice(offset, offset + 100);
    const response = await query(`{ nodes(refs: ${JSON.stringify(batch)}, first: ${batch.length}) { items { requestedRef ref { notePath } node { __typename } error { code message } } pageInfo { truncated } } }`);
    if (response.errors?.length) throw new Error(`ao_index_query_failed: ${response.errors[0].message}`);
    const result = response.data?.nodes;
    if (!Array.isArray(result?.items) || result.pageInfo?.truncated !== false) throw new Error('ao_index_coverage_missing: complete node batch is required');
    const failed = result.items.find(item => item.error);
    if (failed) throw new Error(`ao_index_projection_incomplete: ${failed.requestedRef}: ${failed.error.message}. Check note includes and ignore rules.`);
    const found = new Set(result.items.filter(item => item.node).map(item => item.ref?.notePath));
    const missing = batch.filter(file => !found.has(file));
    if (missing.length) throw new Error(`ao_index_projection_incomplete: ${missing.join(', ')}. Check note includes and ignore rules.`);
  }
  let pending = ['assertion', 'source', 'concept', 'artifact', 'mapOfContent', 'submission', 'curationDocument', 'conflictRegistry'].map(name => ({name, offset: 0}));
  while (pending.length) {
    const response = await query(`{${pending.map(p => `${p.name}(first: 5000, offset: ${p.offset}) { nodeId }`).join('\n')}}`);
    if (response.errors?.length) throw new Error(`ao_index_query_failed: ${response.errors[0].message}`);
    const next = [];
    for (const page of pending) {
      const rows = response.data?.[page.name];
      const coverage = response.extensions?.typedRoots?.[page.name];
      if (!Array.isArray(rows) || !coverage) throw new Error(`ao_index_coverage_missing: ${page.name}`);
      if (coverage.status === 'complete') continue;
      if (coverage.nextOffset > page.offset) next.push({...page, offset: coverage.nextOffset});
      else throw new Error(`ao_index_coverage_incomplete: ${page.name} (${coverage.status})`);
    }
    pending = next;
  }
}
