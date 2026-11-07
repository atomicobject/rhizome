import {mkdir, readFile, rename, rm, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {randomUUID} from 'node:crypto';
import {json, readJSON} from './runtime.mjs';

export async function atomic(file, bytes) {
  await mkdir(path.dirname(file), {recursive: true});
  const temp = `${file}.${randomUUID()}.tmp`;
  try { await writeFile(temp, bytes, {flag: 'wx'}); await rename(temp, file); }
  finally { await rm(temp, {force: true}); }
}
export const manifestPath = (directory, stage) => path.join(directory, stage === 'knowledge' ? 'knowledge-manifest.json' : 'manifest.json');
export async function failure(directory, stage, error) {
  const file = manifestPath(directory, stage);
  const previous = await readJSON(file);
  await atomic(file, json({...previous, formatVersion: 1, compilerVersion: '1', status: previous?.sdlHash ? 'stale' : 'error',
    diagnostics: error.diagnostics || [{code: error.message.split(':')[0], severity: 'error', message: error.message}]}));
}
// Callers hold refresh.lock. Files and manifest form one recoverable generation;
// a crash between renames is always advertised as stale.
export async function publishSet(directory, stage, files, manifest, renameFile = rename) {
  const file = manifestPath(directory, stage);
  const previous = await readJSON(file);
  const names = [...new Set([...Object.keys(files), ...(previous?.generatedFiles || [])])];
  for (const name of names) {
    const allowed = stage === 'knowledge'
      ? ['../ontology/ao-knowledge.generated.graphql', 'selection.json'].includes(name)
      : name === '../ontology/ao-model.generated.graphql' || /^\.\.\/views\/knowledge\/AOKBModel_[A-Za-z0-9_]+\.yaml$/.test(name);
    if (!allowed) throw new Error(`ao_generated_path_invalid: ${name}`);
  }
  const old = new Map();
  for (const name of names) {
    try { old.set(name, await readFile(path.resolve(directory, name))); }
    catch (error) { if (error.code !== 'ENOENT') throw error; old.set(name, null); }
  }
  const staged = [];
  manifest.generatedFiles = Object.keys(files).sort();
  try {
    for (const [name, bytes] of Object.entries(files)) {
      const target = path.resolve(directory, name);
      await mkdir(path.dirname(target), {recursive: true});
      const temp = path.join(directory, 'refresh.lock', `${staged.length}.tmp`);
      await writeFile(temp, bytes);
      staged.push([temp, target]);
    }
    const meta = path.join(directory, 'refresh.lock', 'manifest.tmp');
    await writeFile(meta, json(manifest));
    await atomic(file, json({...manifest, status: 'stale', diagnostics: [{code: 'ao_publish_pending', severity: 'error', message: 'Publication interrupted; refresh bindings again'}]}));
    for (const [temp, target] of staged) await renameFile(temp, target);
    for (const name of names) if (!Object.hasOwn(files, name)) await rm(path.resolve(directory, name), {force: true});
    await renameFile(meta, file);
  } catch (error) {
    for (const [name, bytes] of old) {
      const target = path.resolve(directory, name);
      if (bytes === null) await rm(target, {force: true}); else await atomic(target, bytes);
    }
    if (previous) await atomic(file, json(previous)); else await rm(file, {force: true});
    throw error;
  }
  return manifest;
}
