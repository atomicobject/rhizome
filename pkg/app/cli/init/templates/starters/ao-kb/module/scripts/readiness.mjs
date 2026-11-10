import {readFile, readdir} from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {check as compilerCheck} from '../bindings/publish.mjs';
import {knowledge, siteInventory} from '../bindings/knowledge.mjs';
import {readJSON, adapter} from '../bindings/runtime.mjs';
import {verifyKnowledgeIndex, verifyIndex} from '../bindings/verify-index.mjs';
import {check as prerequisites} from './check.mjs';

const retry = {kind: 'retry', label: 'Retry'};
const blocked = (reason, recovery = [retry]) => ({next: 'blocked', reason, recovery});

// AO owns mode interpretation. Every proposed selection is validated by knowledge().
export async function assess(root, {check = compilerCheck, preflight = prerequisites, verify = async (root, directory, selection, report) => {
  await verifyKnowledgeIndex(root);
  if (report.bindings?.model?.status !== "not applicable") await verifyIndex(root, await adapter(root, directory, selection, {offline: true}));
}} = {}) {
  const directory = path.join(root, '.rhizome/modules/ao-kb/bindings');
  try { await readFile(path.join(root, '.kb/config/vault.md'), 'utf8'); }
  catch (error) { if (error.code !== 'ENOENT') throw error; return blocked('AO KB needs .kb/config/vault.md at this repository root. Install or open the AO knowledge base first.', [{kind: 'install', label: 'Open repository Settings'}, retry]); }
  let selection = await readJSON(path.join(directory, 'selection.json'));
  if (!selection) {
    const candidates = [];
    const rejected = [];
    let dirs = [];
    try { dirs = await readdir(path.join(root, 'products'), {withFileTypes: true}); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    for (const e of dirs.filter(e => e.isDirectory())) {
      try {
        await knowledge(root, directory, {formatVersion: 1, accountRoot: '.', product: e.name, toolkit: null});
        candidates.push(e.name);
      } catch (error) { rejected.push(`${e.name}: ${error.message}`); }
    }
    if (candidates.length > 1) return {next: 'needs-input', reason: 'Choose the product this Knowledge Base will show.', recovery: [{kind: 'choice', label: 'Select product', input: 'product', choices: candidates}]};
    if (candidates.length === 1) return {next: 'prepare', reason: `Preparing ${candidates[0]}`, input: {product: candidates[0]}};
    try { await knowledge(root, directory, {formatVersion: 1, accountRoot: '.', product: null, toolkit: null}); }
    catch (error) { return blocked([error.message, ...rejected].join('\n')); }
    return {next: 'prepare', reason: 'Preparing single-product bindings', input: {single: true}};
  }
  const report = await check({root, directory});
  const rows = preflight(root, {runCompiler: () => report}).checks;
  const fatal = rows.filter(c => ['AO KB root', 'schemaVersion', 'source ownership', 'knowledge bindings'].includes(c.name) && ['error', 'unsupported'].includes(c.status));
  if (fatal.length) return blocked(fatal.map(c => c.message).join('\n'), [{kind: 'review', label: 'Review prerequisite files', paths: fatal.map(c => c.path)}, retry]);
  const diagnostics = report.diagnostics?.filter(d => d.severity === 'error') ?? [];
  const stale = new Set(['ao_knowledge_stale', 'ao_artifact_phases_stale', 'ao_bindings_stale', 'ao_input_changed']);
  if (!report.bindingsReady) {
    if (diagnostics.length && diagnostics.every(d => stale.has(d.code))) return {next: 'prepare', reason: 'Refreshing knowledge bindings'};
    const deps = Object.entries(report.dependencies || {}).filter(([, d]) => !d.ready).map(([name, d]) => `${name}: ${d.message}`);
    // An offline cache miss can be repaired by the ordinary refresh action once.
    if (diagnostics.length && diagnostics.every(d => d.code === 'ao_dependency_unavailable' || stale.has(d.code))) return {next: 'prepare', reason: 'Preparing the pinned AO compiler dependencies'};
    return blocked([...deps, ...diagnostics.map(d => d.message)].join('\n') || 'AO bindings could not be checked. Retry after fixing the compiler prerequisites.');
  }
  const manifest = await readJSON(path.join(directory, 'manifest.json'));
  const includes = [...new Set([...(manifest?.sources ?? []).map(s => s.path), ...await siteInventory(root, selection.product)])];
  if (!report.indexReady) return {next: 'index', reason: 'Publishing the Knowledge Base index', includes};
  try { await verify(root, directory, selection, report); }
  catch (error) { return {next: 'index', reason: `Verifying the current index: ${error.message}`, includes}; }
  return {next: 'ready', reason: 'Knowledge bindings and current index receipts are verified.', includes};
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try { console.log(JSON.stringify(await assess(process.cwd()))); }
  catch (error) { console.log(JSON.stringify(blocked(error.message))); }
}
