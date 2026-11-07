#!/usr/bin/env node
import {parseArgs} from 'node:util';
import {readFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
import {mkdir, realpath, rm} from 'node:fs/promises';
import {refresh, check, atomic} from './publish.mjs';
import {json, readJSON, run, adapter} from './runtime.mjs';
import {verifyIndex} from './verify-index.mjs';

const directory = path.dirname(fileURLToPath(import.meta.url));
try {
  const {values: flags, positionals} = parseArgs({allowPositionals: true, options: {
    root: {type: 'string'}, product: {type: 'string'}, toolkit: {type: 'string'}, single: {type: 'boolean'}}});
  const input = process.stdin.isTTY ? {} : JSON.parse(readFileSync(0, 'utf8').trim() || '{}');
  if (!input || Array.isArray(input) || typeof input !== 'object') throw new Error('ao_action_input: expected a JSON object');
  for (const [key, value] of Object.entries(input)) {
    if (!['root', 'product', 'toolkit', 'single'].includes(key) || typeof value !== (key === 'single' ? 'boolean' : 'string')) throw new Error(`ao_action_input: invalid ${key}`);
  }
  const values = {...input, ...flags};
  const root = await realpath(values.root || path.resolve(directory, '../../../..'));
  const command = positionals[0];
  if (positionals.length !== 1 || !['check', 'refresh-bindings', 'index'].includes(command)) throw new Error('usage: cli.mjs check|refresh-bindings|index [--root account] [--product slug|--single] [--toolkit package-dir]');
  if (values.product && values.single) throw new Error('choose --product or --single');
  if (command !== 'refresh-bindings' && (values.product || values.single || values.toolkit)) throw new Error('selection options belong to refresh-bindings; check and index use the persisted selection');
  if (command === 'refresh-bindings') {
    const previous = await readJSON(path.join(directory, 'selection.json'));
    const selection = {formatVersion: 1, accountRoot: '.', product: values.single ? null : values.product ?? previous?.product ?? null,
      toolkit: values.toolkit ? await realpath(values.toolkit) : previous?.toolkit ?? null};
    const manifest = await refresh({root, directory, selection});
    process.stdout.write(json({starterInstalled: true, bindingsReady: true, indexReady: false, manifest}));
  } else if (command === 'check') {
    const report = await check({root, directory});
    process.stdout.write(json(report));
    if (!report.bindingsReady) process.exitCode = 1;
  } else {
    const lock = path.join(directory, 'refresh.lock');
    await mkdir(lock);
    try {
      const before = await check({root, directory});
      if (!before.bindingsReady) throw new Error('ao_bindings_stale: refresh bindings before indexing');
      const manifest = await readJSON(path.join(directory, 'manifest.json'));
      run(process.env.RZM || 'rzm', ['index'], {cwd: root, timeout: 0});
      const selected = await readJSON(path.join(directory, 'selection.json'));
      await verifyIndex(root, await adapter(root, directory, selected, {offline: true}));
      const after = await check({root, directory});
      const current = await readJSON(path.join(directory, 'manifest.json'));
      if (!after.bindingsReady || current.fingerprint !== manifest.fingerprint) throw new Error('ao_index_inputs_changed: index readiness could not be certified');
      await atomic(path.join(directory, 'index-receipt.json'), json({formatVersion: 1, fingerprint: manifest.fingerprint, sdlHash: manifest.sdlHash}));
      process.stdout.write(json({...after, indexReady: true}));
    } finally { await rm(lock, {recursive: true, force: true}); }
  }
} catch (error) {
  process.stdout.write(json({starterInstalled: true, bindingsReady: false, indexReady: false,
    diagnostics: error.diagnostics || [{code: error.message.split(':')[0], severity: 'error', message: error.message}]}));
  process.exitCode = 1;
}
