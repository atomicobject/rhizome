import {mkdir, readFile, rename, rm, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {randomUUID} from 'node:crypto';
import {json, readJSON, hash, fingerprint, adapter, validateSDL, dependencies} from './runtime.mjs';
import {render} from './render.mjs';

export async function atomic(file, bytes) {
  await mkdir(path.dirname(file), {recursive: true});
  const temp = `${file}.${randomUUID()}.tmp`;
  try { await writeFile(temp, bytes, {flag: 'wx'}); await rename(temp, file); }
  finally { await rm(temp, {force: true}); }
}
const manifestPath = directory => path.join(directory, 'manifest.json');
const sdlPath = directory => path.join(directory, '../ontology/ao-model.generated.graphql');
async function failure(directory, error) {
  const previous = await readJSON(manifestPath(directory));
  const diagnostics = error.diagnostics || [{code: error.message.split(':')[0], severity: 'error', message: error.message}];
  await atomic(manifestPath(directory), json({...previous, formatVersion: 1, compilerVersion: '1',
    status: previous?.sdlHash ? 'stale' : 'error', diagnostics}));
}
export async function refresh({root, directory, selection, exportModel = adapter, validate = validateSDL, beforePublish = async () => {}, renameFile = rename}) {
  const lock = path.join(directory, 'refresh.lock');
  try { await mkdir(lock); }
  catch (error) { if (error.code === 'EEXIST') throw new Error('ao_refresh_locked: another refresh is running; remove refresh.lock only after confirming it stopped'); throw error; }
  try {
    if (!selection || selection.formatVersion !== 1) throw new Error('ao_selection_required: refresh-bindings --product <slug> (or explicit single mode)');
    await atomic(path.join(directory, 'selection.json'), json(selection));
    const ownership = path.join(root, '.rhizome/source-ownership.yml');
    let owned;
    try { owned = await readFile(ownership, 'utf8'); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    if (!owned?.trim()) throw new Error('ao_ownership_missing: run starter setup before refresh-bindings');
    const ir = await exportModel(root, directory, selection);
    const snapshot = await fingerprint(root, directory, ir, selection);
    const result = render(ir);
    await validate(root, result.sdl);
    await beforePublish();
    const current = await exportModel(root, directory, selection);
    const checked = await fingerprint(root, directory, current, selection);
    if (checked.fingerprint !== snapshot.fingerprint || JSON.stringify(current) !== JSON.stringify(ir)) throw new Error('ao_input_changed: inputs changed during compilation');
    const manifest = {formatVersion: 1, compilerVersion: '1', providerContract: ir.providerContract, status: 'ready',
      toolkit: ir.toolkit, context: ir.context, fingerprint: snapshot.fingerprint, inputs: snapshot.inputs,
      sdlHash: hash(result.sdl), sources: ir.sources, knowledgeSources: ir.knowledgeSources, typeMap: result.typeMap, diagnostics: result.diagnostics};
    const file = sdlPath(directory);
    await mkdir(path.dirname(file), {recursive: true});
    const sdlStage = path.join(lock, 'bindings.tmp');
    const manifestStage = path.join(lock, 'manifest.tmp');
    await writeFile(sdlStage, result.sdl);
    await writeFile(manifestStage, json(manifest));
    let oldSDL = null;
    try { oldSDL = await readFile(file); } catch (error) { if (error.code !== 'ENOENT') throw error; }
    const oldManifest = await readJSON(manifestPath(directory));
    if (oldSDL) await atomic(path.join(directory, 'bindings.previous'), oldSDL);
    if (oldManifest) await atomic(path.join(directory, 'manifest.previous'), json(oldManifest));
    // A crash between the two renames is visibly stale, never a ready mismatched pair.
    await atomic(manifestPath(directory), json({...manifest, status: 'stale', diagnostics: [{code: 'ao_publish_pending', severity: 'error', message: 'Binding publication was interrupted; refresh again'}]}));
    try {
      await renameFile(sdlStage, file);
      await renameFile(manifestStage, manifestPath(directory));
    } catch (error) {
      if (oldSDL) await atomic(file, oldSDL); else await rm(file, {force: true});
      if (oldManifest) await atomic(manifestPath(directory), json(oldManifest));
      throw error;
    }
    return manifest;
  } catch (error) {
    await failure(directory, error);
    throw error;
  } finally { await rm(lock, {recursive: true, force: true}); }
}
export async function check({root, directory, exportModel = adapter, checkDependencies = dependencies}) {
  const prerequisites = await checkDependencies();
  const result = {starterInstalled: true, bindingsReady: false, indexReady: false, dependencies: prerequisites, diagnostics: [],
    next: 'rzm ao-kb refresh-bindings; rzm index'};
  const manifest = await readJSON(manifestPath(directory));
  result.status = manifest?.status || 'missing';
  try {
    if (Object.values(prerequisites).some(p => !p.ready)) throw new Error('ao_dependency_missing: install the reported dependencies');
    const selection = await readJSON(path.join(directory, 'selection.json'));
    if (!selection) throw new Error('ao_selection_required: persist a product using refresh-bindings --product <slug>');
    const ir = await exportModel(root, directory, selection, {offline: true});
    const snapshot = await fingerprint(root, directory, ir, selection);
    const rendered = render(ir);
    if (!manifest || manifest.status !== 'ready' || manifest.fingerprint !== snapshot.fingerprint ||
        manifest.sdlHash !== hash(await readFile(sdlPath(directory))) || manifest.sdlHash !== hash(rendered.sdl)) {
      throw new Error('ao_bindings_stale: refresh-bindings is required');
    }
    result.bindingsReady = true;
    result.diagnostics = manifest.diagnostics;
    // Index proof is written only by the explicit index wrapper after a successful rzm index.
    const receipt = await readJSON(path.join(directory, 'index-receipt.json'));
    result.indexReady = receipt?.fingerprint === manifest.fingerprint && receipt?.sdlHash === manifest.sdlHash;
  } catch (error) {
    result.status = manifest?.sdlHash ? 'stale' : 'error';
    result.diagnostics = error.diagnostics || [{code: error.message.split(':')[0], severity: 'error', message: error.message}];
  }
  return result;
}
