import {mkdir, readFile, rm} from 'node:fs/promises';
import path from 'node:path';
import {json, readJSON, hash, fingerprint, adapter, validateSDL, dependencies, indexSignature} from './runtime.mjs';
import {render} from './render.mjs';
import {knowledge, ownership} from './knowledge.mjs';
import {failure, manifestPath, publishSet} from './generation.mjs';
export {atomic} from './generation.mjs';

const sdlPath = (directory, stage) => path.join(directory, `../ontology/ao-${stage}.generated.graphql`);
async function readSDL(directory, stage) {
  try { return await readFile(sdlPath(directory, stage), 'utf8'); }
  catch (error) { if (error.code === 'ENOENT') return ''; throw error; }
}
export async function refresh({root, directory, selection, exportModel = adapter, validate = validateSDL, beforePublish = async () => {}, renameFile}) {
  const lock = path.join(directory, 'refresh.lock');
  try { await mkdir(lock); }
  catch (error) { if (error.code === 'EEXIST') throw new Error('ao_refresh_locked: another refresh is running; remove refresh.lock only after confirming it stopped'); throw error; }
  let stage = 'knowledge';
  try {
    if (!selection || selection.formatVersion !== 1) throw new Error('ao_selection_required: refresh-bindings --product <slug> (or --single true)');
    const scoped = await knowledge(root, directory, selection);
    await validate(root, await readSDL(directory, 'model'), undefined, scoped.sdl);
    const verify = await knowledge(root, directory, selection);
    if (verify.fingerprint !== scoped.fingerprint) throw new Error('ao_input_changed: knowledge configuration changed during compilation');
    await publishSet(directory, 'knowledge', {'../ontology/ao-knowledge.generated.graphql': scoped.sdl, 'selection.json': json(selection)},
      {formatVersion: 1, status: 'ready', context: scoped.context, fingerprint: scoped.fingerprint, inputs: scoped.inputs, sdlHash: hash(scoped.sdl), diagnostics: []}, renameFile);
    stage = 'model';
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
    return await publishSet(directory, 'model', files, manifest, renameFile);
  } catch (error) {
    await failure(directory, stage, error);
    error.knowledgeReady = stage === 'model';
    throw error;
  } finally { await rm(lock, {recursive: true, force: true}); }
}
export async function check({root, directory, exportModel = adapter, checkDependencies = dependencies}) {
  const prerequisites = await checkDependencies();
  const result = {starterInstalled: true, bindingsReady: false, knowledgeReady: false, modelReady: false, indexReady: false,
    bindings: {knowledge: {ready: false, status: 'missing'}, model: {ready: false, status: 'missing'}}, dependencies: prerequisites, diagnostics: [],
    next: 'rzm ao-kb refresh-bindings --product <slug> (or --single true); node .rhizome/modules/ao-kb/bindings/cli.mjs index'};
  const selection = await readJSON(path.join(directory, 'selection.json'));
  const manifest = await readJSON(manifestPath(directory, 'model'));
  result.status = manifest?.status || 'missing';
  try {
    const scoped = await knowledge(root, directory, selection);
    const saved = await readJSON(manifestPath(directory, 'knowledge'));
    if (!saved || saved.status !== 'ready' || saved.fingerprint !== scoped.fingerprint || saved.sdlHash !== hash(await readSDL(directory, 'knowledge')) || saved.sdlHash !== hash(scoped.sdl)) throw new Error('ao_knowledge_stale: refresh-bindings is required');
    result.knowledgeReady = true;
    result.bindings.knowledge = {ready: true, status: 'ready', product: scoped.context.product};
  } catch (error) {
    result.bindings.knowledge.status = 'incomplete';
    result.diagnostics.push(...(error.diagnostics || [{code: error.message.split(':')[0], severity: 'error', message: error.message}]));
  }
  try {
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
    result.indexReady = result.knowledgeReady && receipt?.fingerprint === manifest.fingerprint && receipt?.sdlHash === manifest.sdlHash && receipt?.sourceSignature === await indexSignature(root, ir);
  } catch (error) {
    result.status = manifest?.sdlHash ? 'stale' : 'error';
    result.bindings.model.status = error.diagnostics?.some(d => d.severity === 'error') ? 'incomplete' : result.status;
    result.diagnostics.push(...(error.diagnostics || [{code: error.message.split(':')[0], severity: 'error', message: error.message}]));
  }
  result.bindingsReady = result.knowledgeReady && result.modelReady;
  if (result.bindingsReady) result.status = 'ready';
  if (result.bindingsReady && !result.indexReady) result.next = 'node .rhizome/modules/ao-kb/bindings/cli.mjs index';
  return result;
}
