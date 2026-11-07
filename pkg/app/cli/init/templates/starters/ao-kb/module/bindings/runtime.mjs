import {spawnSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {readFile, readdir, realpath, mkdtemp, mkdir, writeFile, rm} from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';

export const hash = bytes => 'sha256:' + createHash('sha256').update(bytes).digest('hex');
export const json = value => JSON.stringify(value, null, 2) + '\n';
export async function readJSON(file) {
  try { return JSON.parse(await readFile(file, 'utf8')); }
  catch (error) { if (error.code === 'ENOENT') return null; throw error; }
}
export function run(command, args, options = {}) {
  const result = spawnSync(command, args, {encoding: 'utf8', maxBuffer: 64 * 1024 * 1024, timeout: 120000, ...options});
  if (result.error || result.status !== 0) throw Object.assign(new Error(`${path.basename(command)} failed: ${result.error?.message || result.stdout || result.stderr}`), {stdout: result.stdout});
  return result.stdout;
}
export async function adapter(root, directory, selection, {offline = false} = {}) {
  const pin = await readJSON(path.join(directory, 'toolkit-pin.json'));
  let spec = `${pin.source}@${pin.revision}#subdirectory=packages/kb-site`;
  if (selection.toolkit) {
    spec = await realpath(selection.toolkit);
    const revision = run('git', ['-C', spec, 'rev-parse', 'HEAD']).trim();
    if (revision !== pin.revision) throw new Error(`ao_toolkit_revision: expected ${pin.revision}`);
  }
  // uv builds/caches the pinned package outside the checkout; never creates AO's .venv.
  const argv = ['run', ...(offline ? ['--offline'] : []), '--no-project', '--with', spec,
    '--with', 'pyyaml==6.0.3', 'python', path.join(directory, 'adapter.py'), '--root', root];
  if (selection.product) argv.push('--product', selection.product);
  try { return JSON.parse(run('uv', argv, {cwd: root})); }
  catch (error) {
    let payload;
    try { payload = JSON.parse(error.stdout); } catch { throw error; }
    throw Object.assign(new Error(payload.diagnostics?.[0]?.message || error.message), {diagnostics: payload.diagnostics});
  }
}
export async function fingerprint(root, directory, ir, selection) {
  const inputs = [...ir.inputs];
  for (const file of [...await schemaFiles(root), path.join(root, '.rhizome/config.yml')]) {
    let bytes = null;
    try { bytes = await readFile(file); } catch (error) { if (error.code !== 'ENOENT') throw error; }
    inputs.push({path: path.relative(root, file).split(path.sep).join('/'), hash: bytes === null ? null : hash(bytes)});
  }
  for (const entry of await readdir(directory)) {
    if (/\.(mjs|py|json)$/.test(entry) && !['manifest.json', 'selection.json', 'index-receipt.json'].includes(entry)) {
      inputs.push({path: `@compiler/${entry}`, hash: hash(await readFile(path.join(directory, entry)))});
    }
  }
  for (const input of ir.inputs) {
    const full = path.resolve(root, input.path);
    if (!full.startsWith(root + path.sep)) throw new Error(`ao_input_escape: ${input.path}`);
    let current = null;
    try {
      const resolved = await realpath(full);
      if (!resolved.startsWith(root + path.sep)) throw new Error(`ao_input_escape: ${input.path}`);
      current = hash(await readFile(full));
    } catch (error) { if (error.code !== 'ENOENT') throw error; }
    if (current !== input.hash) throw new Error(`ao_input_changed: ${input.path}`);
  }
  inputs.sort((a, b) => a.path < b.path ? -1 : a.path > b.path ? 1 : 0);
  return {inputs, fingerprint: hash(json({inputs, selection, toolkit: ir.toolkit, contract: ir.providerContract, root}))};
}
async function schemaFiles(root) {
  const dirs = [path.join(root, '.rhizome/ontology')];
  try {
    for (const entry of await readdir(path.join(root, '.rhizome/modules'), {withFileTypes: true})) {
      if (entry.isDirectory()) dirs.push(path.join(root, '.rhizome/modules', entry.name, 'ontology'));
    }
  } catch (error) { if (error.code !== 'ENOENT') throw error; }
  const files = [];
  for (const dir of dirs) {
    let entries;
    try { entries = await readdir(dir); } catch (error) { if (error.code === 'ENOENT') continue; throw error; }
    for (const file of entries) if (file.endsWith('.graphql') && file !== 'ao-model.generated.graphql') files.push(path.join(dir, file));
  }
  return files.sort();
}
export async function validateSDL(root, sdl, binary = process.env.RZM || 'rzm') {
  const scratch = await mkdtemp(path.join(os.tmpdir(), 'aokb-bindings-'));
  try {
    const target = path.join(scratch, '.rhizome/ontology');
    await mkdir(target, {recursive: true});
    await writeFile(path.join(scratch, '.rhizome/config.yml'), '{}\n');
    let count = 0;
    for (const file of await schemaFiles(root)) await writeFile(path.join(target, `schema-${count++}.graphql`), await readFile(file));
    await writeFile(path.join(target, 'ao-model.generated.graphql'), sdl);
    run(binary, ['ontology', 'query-schema'], {cwd: scratch});
  } finally { await rm(scratch, {recursive: true, force: true}); }
}
export async function dependencies() {
  const checks = {};
  for (const name of ['uv', process.env.RZM || 'rzm']) {
    try { checks[path.basename(name)] = {ready: true, version: run(name, ['--version']).trim()}; }
    catch (error) { checks[path.basename(name)] = {ready: false, message: error.message}; }
  }
  return checks;
}
