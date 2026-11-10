#!/usr/bin/env node
import {parseArgs} from 'node:util';
import {readFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
import {realpath} from 'node:fs/promises';
import {refresh, check, atomic} from './publish.mjs';
import {json, readJSON, run, adapter} from './runtime.mjs';
import {indexSignature} from './knowledge.mjs';
import {verifyIndex, verifyKnowledgeIndex} from './verify-index.mjs';
import {acquire} from './lock.mjs';

const directory = path.dirname(fileURLToPath(import.meta.url));
try {
  const {values: flags, positionals} = parseArgs({allowPositionals: true, options: {
    root: {type: 'string'}, product: {type: 'string'}, toolkit: {type: 'string'}, single: {type: 'boolean'}}});
  let input = {};
  // Actions write their input to a blocking pipe; a non-blocking empty stdin (agent shells) is direct CLI use.
  try { if (!process.stdin.isTTY) input = JSON.parse(readFileSync(0, 'utf8').trim() || '{}'); }
  catch (error) { if (error.code !== 'EAGAIN') throw error; }
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
    // Report the state the check computes, never an assumed one.
    const report = await check({root, directory});
    process.stdout.write(json({...report, manifest}));
    if (!report.bindingsReady) process.exitCode = 1;
  } else if (command === 'check') {
    const report = await check({root, directory});
    process.stdout.write(json(report));
    if (!report.bindingsReady) process.exitCode = 1;
  } else {
    const lock = await acquire(directory);
    try {
      const before = await check({root, directory});
      if (!before.bindingsReady) throw new Error('ao_bindings_stale: refresh bindings before indexing');
      // Below schemaVersion 8 the model is not applicable: no model export or model-record verification.
      const model = before.bindings.model.status !== 'not applicable';
      const manifest = await readJSON(path.join(directory, 'manifest.json'));
      const knowledge = await readJSON(path.join(directory, 'knowledge-manifest.json'));
      const selected = await readJSON(path.join(directory, 'selection.json'));
      const signed = async () => indexSignature(root, model ? await adapter(root, directory, selected, {offline: true}) : null, selected.product);
      const signature = await signed();
      run(process.env.RZM || 'rzm', ['index'], {cwd: root, timeout: 0});
      if (model) await verifyIndex(root, await adapter(root, directory, selected, {offline: true}));
      await verifyKnowledgeIndex(root);
      const after = await check({root, directory});
      const current = await readJSON(path.join(directory, 'manifest.json'));
      const currentKnowledge = await readJSON(path.join(directory, 'knowledge-manifest.json'));
      const sourceSignature = await signed();
      if (!after.bindingsReady || current?.fingerprint !== manifest?.fingerprint || currentKnowledge.fingerprint !== knowledge.fingerprint || signature !== sourceSignature) throw new Error('ao_index_inputs_changed: index inputs changed while indexing; run rzm ao-kb refresh-bindings, then node .rhizome/modules/ao-kb/bindings/cli.mjs index. No index receipt was issued');
      await atomic(path.join(directory, 'index-receipt.json'), json(model
        ? {formatVersion: 1, fingerprint: manifest.fingerprint, knowledgeFingerprint: knowledge.fingerprint, sdlHash: manifest.sdlHash, sourceSignature}
        : {formatVersion: 1, model: 'not applicable', knowledgeFingerprint: knowledge.fingerprint, sourceSignature}));
      process.stdout.write(json({...after, indexReady: true}));
    } finally { await lock.release(); }
  }
} catch (error) {
  process.stdout.write(json({starterInstalled: true, bindingsReady: false, indexReady: false,
    knowledgeReady: Boolean(error.knowledgeReady), modelReady: false,
    bindings: {knowledge: {ready: Boolean(error.knowledgeReady), status: error.knowledgeReady ? 'ready' : 'incomplete'}, model: {ready: false, status: 'incomplete'}},
    diagnostics: error.diagnostics || [{code: error.message.split(':')[0], severity: 'error', message: error.message}]}));
  console.error(error.diagnostics?.map(d => d.message).join("\n") || error.message);
  process.exitCode = 1;
}
