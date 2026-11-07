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

/** Read the policy's block sources and scalar path lists; uncertain YAML certifies no ownership. */
export function ownedPatterns(text) {
  if ((text?.match(/^sources:[ \t]*$/gm) || []).length !== 1) return [];
  let ownerIndent;
  for (const line of text.split('\n')) {
    if (!line.trim() || /^\s*#/.test(line) || /^sources:[ \t]*$/.test(line)) continue;
    const indent = line.match(/^ */)[0].length;
    if (/^ *- +owner:/.test(line)) {
      if (ownerIndent !== undefined && indent !== ownerIndent) return [];
      ownerIndent = indent;
    } else if (/^ +(?:reason|paths):/.test(line)) {
      if (ownerIndent === undefined || indent !== ownerIndent + 2) return [];
    } else if (/^ +- /.test(line)) {
      if (ownerIndent === undefined || indent !== ownerIndent + 4) return [];
    } else return [];
  }
  const scalar = raw => {
    if (raw === undefined) return null;
    const value = raw.trim().replace(/\s+#.*$/, '');
    if (value.startsWith('"')) {
      try { const parsed = JSON.parse(value); return typeof parsed === 'string' && parsed.trim() ? parsed : null; }
      catch { return null; }
    }
    if (value.startsWith("'")) {
      if (!/^'(?:[^']|'')*'$/.test(value)) return null;
      const parsed = value.slice(1, -1).replace(/''/g, "'");
      return parsed.trim() ? parsed : null;
    }
    if (!value || /^(?:null|~)$/i.test(value) || /^[&*!|>#]/.test(value) || /^[-?:]\s/.test(value) || /[\[\]{}]|:\s/.test(value)) return null;
    return value;
  };
  const patterns = [];
  for (const block of text.split(/^ *- +owner:[ \t]*/m).slice(1)) {
    if ((block.match(/^ +reason:/gm) || []).length !== 1 || (block.match(/^ +paths:/gm) || []).length !== 1) return [];
    if (!scalar(block.split('\n')[0]) || !scalar(block.match(/^ +reason:[ \t]*([^\n]+)/m)?.[1])) return [];
    const value = block.match(/^ +paths:[ \t]*(\[[^\n]*\]|\n(?: +- [^\n]*\n?)*)/m)?.[1];
    if (!value) return [];
    const entries = value.startsWith('[') ? value.slice(1, -1).trim() ? value.slice(1, -1).split(',') : []
      : value.split('\n').filter(s => s.trim()).map(s => s.replace(/^ +- /, ''));
    const decoded = entries.map(scalar);
    if (decoded.some(value => value === null || /[\[\]{}\\\x00-\x1f]/.test(value))) return [];
    patterns.push(...decoded);
  }
  return patterns;
}

/** Matches the preflight's bounded ownership patterns, ignoring case.
 * shortcut: brace/class policies are not certified; extend parsing before supporting those patterns. */
export function globMatch(pattern, path) {
  let re = '';
  for (let i = 0; i < pattern.length; i++) {
    const c = pattern[i];
    if (c === '*' && pattern[i + 1] === '*') {
      const recursive = (i === 0 || pattern[i - 1] === '/') && (i + 2 === pattern.length || pattern[i + 2] === '/');
      i++;
      if (!recursive) { re += '[^/]*'; continue; }
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
  const detail = (report.diagnostics ?? []).filter(d => d.severity === 'error').map(d => d.message || d.code).join(' ');
  // The compiler's state names what failed; a missing dependency or duplicate IDs is never reported as stale.
  const dependency = report.status === 'dependency missing' || codes.includes('ao_dependency_missing');

  const deps = Object.entries(report.dependencies ?? {});
  if (deps.length) {
    const missing = deps.filter(([, d]) => !d.ready).map(([name]) => name);
    const found = deps.map(([name, d]) => `${name} ${d.version || ''}`.trim()).join(', ');
    add('model compiler dependencies', missing.length || dependency ? 'dependency missing' : 'ok', missing.length
      ? `Missing ${missing.join(' and ')}. The binding compiler needs uv on PATH (it supplies Python and AO's pinned kb-site package) and the Rhizome executable.`
      : dependency ? `Found ${found}, but the compiler's Python packages are unavailable. ${detail}` : `Found ${found}.`, `${MODULE}/bindings`);
  }

  const bindingsPath = `${MODULE}/bindings/manifest.json`;
  if (report.bindingsReady) {
    add('model bindings', 'ok', `Bindings match the selected product's model configuration. Authored values refresh through normal indexing.`, bindingsPath);
  } else if (codes.includes('ao_selection_required')) {
    add('model bindings', 'pending', `No product selected yet. ${SELECT}`, bindingsPath);
  } else if (dependency) {
    add('model bindings', 'dependency missing', `Bindings cannot be checked until the compiler dependencies are available; any previous bindings stay in place. ${detail}`.trim(), bindingsPath);
  } else if (report.status === 'duplicate IDs') {
    add('model bindings', 'duplicate IDs', `AO model records share an ID, so refresh keeps the previous bindings instead of choosing one. ${detail} Make each ID unique in its AO source, then run \`rzm ao-kb refresh-bindings\`.`, bindingsPath);
  } else if (report.status === 'stale') {
    add('model bindings', 'stale', `Bindings are stale: run \`rzm ao-kb refresh-bindings\`. ${detail}`.trim(), bindingsPath);
  } else {
    add('model bindings', 'error', detail || 'The binding compiler reported an error.', bindingsPath);
  }

  add('model index', report.indexReady ? 'ok' : 'pending', report.indexReady
    ? 'A verified index receipt matches these bindings.'
    : 'No verified index receipt for the current bindings; `node .rhizome/modules/ao-kb/bindings/cli.mjs index` indexes and writes one.', `${MODULE}/bindings/index-receipt.json`);

  add('knowledge admission', report.knowledgeReady ? 'ok' : 'pending', report.knowledgeReady
    ? 'The generated knowledge admission matches the persisted product selection.'
    : `Knowledge typing is unscoped or stale until setup completes. ${SELECT}`, `${MODULE}/ontology/ao-knowledge.generated.graphql`);

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
