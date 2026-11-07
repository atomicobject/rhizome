// The model-binding half of the AO KB check: runs the binding compiler's own read-only
// check and states its result as named rows beside the knowledge checks. The compiler's
// computed status, not its saved manifest, decides freshness.
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const MODULE = '.rhizome/modules/ao-kb';
const OWNERSHIP = '.rhizome/source-ownership.yml';

/** The compiler's `check` report, or an error report when it cannot run. */
export function runCompilerCheck(root) {
  const cli = join(root, MODULE, 'bindings/cli.mjs');
  if (!existsSync(cli)) {
    return { bindingsReady: false, indexReady: false, status: 'missing', diagnostics: [{ code: 'ao_compiler_missing', severity: 'error', message: 'The binding compiler is not installed; re-run init with the ao-kb add-on.' }] };
  }
  const result = spawnSync(process.execPath, [cli, 'check'], { cwd: root, input: '{}', encoding: 'utf8', timeout: 170_000 });
  try {
    return JSON.parse(result.stdout);
  } catch {
    const tail = `${result.stderr || ''}${result.error?.message || ''}`.trim().slice(-400);
    return { bindingsReady: false, indexReady: false, status: 'error', diagnostics: [{ code: 'ao_compiler_failed', severity: 'error', message: `The binding compiler check did not report a result. ${tail}`.trim() }] };
  }
}

/** Path patterns listed in the ownership declaration (simple `- path` list items). */
export function ownedPatterns(text) {
  return (text ?? '').split('\n').map(line => line.match(/^\s*-\s*['"]?([^'"#\s]+)['"]?\s*(?:#.*)?$/)?.[1]).filter(Boolean);
}

/** Matches a vault path against an ownership pattern the way the declaration does, ignoring case.
 * ponytail: covers `*`, `**`, and `?`; brace and class patterns read literally. */
export function globMatch(pattern, path) {
  let re = '';
  for (let i = 0; i < pattern.length; i++) {
    const c = pattern[i];
    if (c === '*' && pattern[i + 1] === '*') {
      i++;
      if (pattern[i + 1] === '/') { i++; re += '(?:.*/)?'; } else re += '.*';
    } else if (c === '*') re += '[^/]*';
    else if (c === '?') re += '[^/]';
    else re += c.replace(/[.+^${}()|[\]\\]/g, '\\$&');
  }
  return new RegExp(`^${re}$`, 'i').test(path);
}

function read(root, path) {
  try { return readFileSync(join(root, path), 'utf8'); } catch (error) { if (error.code === 'ENOENT') return null; throw error; }
}

const SELECT = "Select a product with `rzm ao-kb refresh-bindings --product <slug>`, or `--single true` for a single-product KB.";

export function bindingRows(root, report) {
  const rows = [];
  const add = (name, status, message, path) => rows.push({ name, status, message, path });
  const codes = (report.diagnostics ?? []).map(d => d.code);
  const detail = (report.diagnostics ?? []).filter(d => d.severity === 'error').map(d => d.message).join(' ');

  const deps = Object.entries(report.dependencies ?? {});
  if (deps.length) {
    const missing = deps.filter(([, d]) => !d.ready).map(([name]) => name);
    add('model compiler dependencies', missing.length ? 'error' : 'ok', missing.length
      ? `Missing ${missing.join(' and ')}. The binding compiler needs uv on PATH (it supplies Python and AO's pinned kb-site package) and the Rhizome executable.`
      : `Found ${deps.map(([name, d]) => `${name} ${d.version || ''}`.trim()).join(', ')}.`, `${MODULE}/bindings`);
  }

  const bindingsPath = `${MODULE}/bindings/manifest.json`;
  if (report.bindingsReady) {
    add('model bindings', 'ok', `Bindings match the selected product's current model. Model records are not indexed or shown in this release.`, bindingsPath);
  } else if (codes.includes('ao_selection_required')) {
    add('model bindings', 'pending', `No product selected yet. ${SELECT}`, bindingsPath);
  } else if (codes.includes('ao_dependency_missing')) {
    add('model bindings', 'pending', 'Install the missing compiler dependencies, then refresh bindings.', bindingsPath);
  } else if (report.status === 'stale') {
    add('model bindings', 'warning', `Bindings are stale: run \`rzm ao-kb refresh-bindings\`. ${detail}`.trim(), bindingsPath);
  } else {
    add('model bindings', 'error', detail || 'The binding compiler reported an error.', bindingsPath);
  }

  add('model index', report.indexReady ? 'ok' : 'pending', report.indexReady
    ? 'A verified index receipt matches these bindings.'
    : 'No verified index receipt for the current bindings; `node .rhizome/modules/ao-kb/bindings/cli.mjs index` indexes and writes one.', `${MODULE}/bindings/index-receipt.json`);

  const manifest = JSON.parse(read(root, bindingsPath) ?? 'null');
  if (!manifest?.sources && !manifest?.knowledgeSources) {
    add('model source ownership', 'pending', 'Checked after the first successful refresh lists the model sources.', OWNERSHIP);
  } else {
    const patterns = ownedPatterns(read(root, OWNERSHIP));
    const paths = [...(manifest.sources ?? []), ...(manifest.knowledgeSources ?? [])].map(s => s.path).filter(Boolean);
    const unowned = [...new Set(paths.filter(path => !patterns.some(p => globMatch(p, path))))];
    add('model source ownership', unowned.length ? 'error' : 'ok', unowned.length
      ? `These compiled AO sources are not declared AO-owned, so Rhizome could edit them: ${unowned.join(', ')}. Add them to ${OWNERSHIP} before indexing.`
      : `All ${new Set(paths).size} compiled model and knowledge sources are declared AO-owned.`, OWNERSHIP);
  }
  return rows;
}
