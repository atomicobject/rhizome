import {readFile, rm} from 'node:fs/promises';
import path from 'node:path';
import {json, readJSON, hash, fingerprint, adapter, validateSDL, dependencies} from './runtime.mjs';
import {render} from './render.mjs';
import {knowledge, ownership, indexSignature, schemaVersion, notApplicable, MODEL_SCHEMA} from './knowledge.mjs';
import {atomic, failure, manifestPath, publishSet} from './generation.mjs';
import {acquire} from './lock.mjs';
export {atomic} from './generation.mjs';

const sdlPath = (directory, stage) => path.join(directory, `../ontology/ao-${stage}.generated.graphql`);
async function readSDL(directory, stage) {
  try { return await readFile(sdlPath(directory, stage), 'utf8'); }
  catch (error) { if (error.code === 'ENOENT') return ''; throw error; }
}
export async function refresh({root, directory, selection, exportModel = adapter, validate = validateSDL, beforePublish = async () => {}, renameFile}) {
  const lock = await acquire(directory);
  let stage = 'knowledge';
  // A product switch must not leave the new knowledge scope beside the previous product's model.
  let switching = false;
  const previousKnowledge = {};
  try {
    const previousModel = await readJSON(manifestPath(directory, 'model'));
    switching = Boolean(previousModel?.sdlHash) && (previousModel.context?.product ?? null) !== (selection?.product ?? null);
    for (const name of ['../ontology/ao-knowledge.generated.graphql', 'selection.json', path.basename(manifestPath(directory, 'knowledge'))]) {
      try { previousKnowledge[name] = await readFile(path.resolve(directory, name)); }
      catch (error) { if (error.code !== 'ENOENT') throw error; previousKnowledge[name] = null; }
    }
    if (!selection || selection.formatVersion !== 1) throw new Error('ao_selection_required: refresh-bindings --product <slug> (or --single true)');
    const scoped = await knowledge(root, directory, selection);
    await validate(root, await readSDL(directory, 'model'), undefined, scoped.sdl);
    const verify = await knowledge(root, directory, selection);
    if (verify.fingerprint !== scoped.fingerprint) throw new Error('ao_input_changed: knowledge configuration changed during compilation');
    await publishSet(directory, 'knowledge', {'../ontology/ao-knowledge.generated.graphql': scoped.sdl, 'selection.json': json(selection)},
      {formatVersion: 1, status: 'ready', context: scoped.context, artifactPhases: scoped.artifactPhases, fingerprint: scoped.fingerprint, inputs: scoped.inputs, sdlHash: hash(scoped.sdl), diagnostics: scoped.notes}, lock.directory, renameFile);
    stage = 'model';
    // Below schemaVersion 8 the model exporter never runs; any previous model generation is withdrawn.
    if (scoped.schemaVersion < MODEL_SCHEMA) return await publishSet(directory, 'model', {},
      {formatVersion: 1, compilerVersion: '1', status: 'not applicable', reason: notApplicable(scoped.schemaVersion), context: scoped.context, diagnostics: []}, lock.directory, renameFile);
    const ir = await exportModel(root, directory, selection);
    await ownership(root, ir.sources.map(s => s.path));
    const snapshot = await fingerprint(root, directory, ir, selection);
    const result = render(ir);
    await validate(root, result.sdl, undefined, scoped.sdl);
    await beforePublish();
    const current = await exportModel(root, directory, selection);
    const checked = await fingerprint(root, directory, current, selection);
    const currentScope = await knowledge(root, directory, selection);
    if (currentScope.fingerprint !== scoped.fingerprint || checked.fingerprint !== snapshot.fingerprint || JSON.stringify(current) !== JSON.stringify(ir)) throw new Error('ao_input_changed: inputs changed during compilation');
    const manifest = {formatVersion: 1, compilerVersion: '1', providerContract: ir.providerContract, status: 'ready',
      toolkit: ir.toolkit, context: ir.context, fingerprint: snapshot.fingerprint, inputs: snapshot.inputs,
      sdlHash: hash(result.sdl), sources: ir.sources, knowledgeSources: ir.knowledgeSources, typeMap: result.typeMap, diagnostics: result.diagnostics};
    const files = {'../ontology/ao-model.generated.graphql': result.sdl,
      ...Object.fromEntries(Object.entries(result.views).map(([name, bytes]) => [`../views/knowledge/${name}`, bytes]))};
    return await publishSet(directory, 'model', files, manifest, lock.directory, renameFile);
  } catch (error) {
    if (switching && stage === 'model') {
      for (const [name, bytes] of Object.entries(previousKnowledge)) {
        if (bytes === null) await rm(path.resolve(directory, name), {force: true}); else await atomic(path.resolve(directory, name), bytes);
      }
      error.message = `${error.message} (product switch not applied; the previous product's bindings remain)`;
    }
    await failure(directory, stage, error);
    error.knowledgeReady = stage === 'model';
    throw error;
  } finally { await lock.release(); }
}
// A failed check names its actual state: a missing dependency or duplicate source IDs is not staleness.
const STALE = new Set(['ao_bindings_stale', 'ao_knowledge_stale', 'ao_artifact_phases_stale', 'ao_input_changed']);
const DEPENDENCY = new Set(['ao_dependency_missing', 'ao_dependency_unavailable', 'ao_python_dependency']);
function state(diagnostics) {
  const codes = diagnostics.filter(d => d.severity === 'error').map(d => d.code);
  if (codes.some(code => DEPENDENCY.has(code))) return 'dependency missing';
  if (codes.includes('ao_duplicate_record')) return 'duplicate IDs';
  if (codes.includes('ao_selection_required')) return 'selection required';
  return codes.length && codes.every(code => STALE.has(code)) ? 'stale' : 'error';
}
async function vaultSchema(root) {
  try { return schemaVersion(await readFile(path.join(root, '.kb/config/vault.md'), 'utf8')); }
  catch (error) { if (error.code === 'ENOENT') return MODEL_SCHEMA; throw error; }
}
export async function check({root, directory, exportModel = adapter, checkDependencies = dependencies}) {
  const version = await vaultSchema(root);
  // Below schemaVersion 8 the model is not applicable: no model dependency is required or looked up.
  const legacy = version < MODEL_SCHEMA;
  const prerequisites = legacy ? {} : await checkDependencies();
  const result = {starterInstalled: true, bindingsReady: false, knowledgeReady: false, modelReady: false, indexReady: false,
    bindings: {knowledge: {ready: false, status: 'missing'}, model: {ready: false, status: 'missing'}}, dependencies: prerequisites, diagnostics: [],
    next: 'rzm ao-kb refresh-bindings --product <slug> (or --single true); node .rhizome/modules/ao-kb/bindings/cli.mjs index'};
  const selection = await readJSON(path.join(directory, 'selection.json'));
  const manifest = await readJSON(manifestPath(directory, 'model'));
  const knowledgeManifest = await readJSON(manifestPath(directory, 'knowledge'));
  try {
    const scoped = await knowledge(root, directory, selection);
    const saved = knowledgeManifest;
    if (saved && JSON.stringify(saved.artifactPhases) !== JSON.stringify(scoped.artifactPhases)) throw new Error(`ao_artifact_phases_stale: artifact phase folder inventory is missing or changed; previous ${JSON.stringify(saved.artifactPhases)}, current ${JSON.stringify(scoped.artifactPhases)}. Run rzm ao-kb refresh-bindings before indexing`);
    if (!saved || saved.status !== 'ready' || saved.fingerprint !== scoped.fingerprint || saved.sdlHash !== hash(await readSDL(directory, 'knowledge')) || saved.sdlHash !== hash(scoped.sdl)) throw new Error('ao_knowledge_stale: refresh-bindings is required');
    result.knowledgeReady = true;
    result.bindings.knowledge = {ready: true, status: 'ready', product: scoped.context.product, artifactPhases: scoped.artifactPhases};
  } catch (error) {
    result.bindings.knowledge.status = 'incomplete';
    result.diagnostics.push(...(error.diagnostics || [{code: error.message.split(':')[0], severity: 'error', message: error.message}]));
  }
  if (legacy) {
    result.bindings.model = {ready: false, status: 'not applicable', reason: notApplicable(version)};
    const receipt = await readJSON(path.join(directory, 'index-receipt.json'));
    result.indexReady = result.knowledgeReady && receipt?.knowledgeFingerprint === knowledgeManifest?.fingerprint &&
      receipt?.sourceSignature === await indexSignature(root, null, selection?.product);
  } else try {
    if (!selection) throw new Error('ao_selection_required: run rzm ao-kb refresh-bindings --product <slug> (or --single true)');
    if (Object.values(prerequisites).some(p => !p.ready)) throw new Error('ao_dependency_missing: install the reported dependencies');
    const ir = await exportModel(root, directory, selection, {offline: true});
    await ownership(root, ir.sources.map(s => s.path));
    // Compatibility is re-evaluated on every check, independent of freshness.
    const rendered = render(ir);
    const snapshot = await fingerprint(root, directory, ir, selection);
    if (!manifest || manifest.status !== 'ready' || manifest.fingerprint !== snapshot.fingerprint ||
        manifest.sdlHash !== hash(await readSDL(directory, 'model')) || manifest.sdlHash !== hash(rendered.sdl) ||
        await Promise.all(Object.entries(rendered.views).map(async ([name, bytes]) => {
          try { return hash(await readFile(path.join(directory, '../views/knowledge', name))) === hash(bytes); }
          catch (error) { if (error.code === 'ENOENT') return false; throw error; }
        })).then(values => values.some(value => !value))) throw new Error('ao_bindings_stale: refresh-bindings is required');
    result.modelReady = true;
    result.bindings.model = {ready: true, status: 'ready'};
    result.diagnostics.push(...manifest.diagnostics);
    const receipt = await readJSON(path.join(directory, 'index-receipt.json'));
    result.indexReady = result.knowledgeReady && receipt?.knowledgeFingerprint === knowledgeManifest?.fingerprint && receipt?.fingerprint === manifest.fingerprint && receipt?.sdlHash === manifest.sdlHash && receipt?.sourceSignature === await indexSignature(root, ir, selection.product);
  } catch (error) {
    const diagnostics = error.diagnostics || [{code: error.message.split(':')[0], severity: 'error', message: error.message}];
    result.bindings.model.status = error.diagnostics?.some(d => d.severity === 'error') ? 'incomplete' : state(diagnostics);
    result.diagnostics.push(...diagnostics);
  }
  result.bindingsReady = result.knowledgeReady && (result.modelReady || result.bindings.model.status === 'not applicable');
  result.status = result.bindingsReady ? 'ready' : state(result.diagnostics);
  if (result.bindingsReady && !result.indexReady) result.next = 'node .rhizome/modules/ao-kb/bindings/cli.mjs index';
  return result;
}
