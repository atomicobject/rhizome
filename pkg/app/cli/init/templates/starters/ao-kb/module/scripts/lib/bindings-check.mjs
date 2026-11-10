// The model-binding half of the AO KB check: runs the binding compiler's own read-only
// check and states its result as named rows beside the knowledge checks. The compiler's
// computed status, not its saved manifest, decides freshness. It also reads the source
// ownership declaration: the starter once declared AO KB's layout read-only, and refresh
// lifts that former declaration from an installation that still carries it.
import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';

const MODULE = '.rhizome/modules/ao-kb';

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

// One plain or quoted YAML scalar, or null when it is anything else.
function scalar(raw) {
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
}

/** Read the policy's block sources and scalar path lists; uncertain YAML certifies no ownership. */
export function ownedPatterns(text) {
  // A Windows checkout (core.autocrlf) or editor can write CRLF; `$` would not match before `\r`.
  text = text?.replace(/\r\n/g, '\n');
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

/** The starter's former declaration, installed before 2026-11-10: it kept AO KB's standard
 * layout read-only in Rhizome. Refresh lifts exactly these paths from an `AO KB` entry. */
export const STARTER_OWNER = 'AO KB';
export const STARTER_PATHS = ['.kb/**', 'assertions/**', 'sources/**', 'concepts/**', 'artifacts/**', 'decisions.md', '_moc.md', 'MOCs/**', '_improvements/**',
  'products/*/.kb/**', 'products/*/assertions/**', 'products/*/sources/**', 'products/*/concepts/**', 'products/*/artifacts/**', 'products/*/decisions.md', 'products/*/_moc.md', 'products/*/MOCs/**', 'products/*/_improvements/**'];

/** `text` without the starter's former AO KB paths, and which of them it listed. Every other
 * line stays byte for byte: other owners' entries, and paths someone added under AO KB. An
 * AO KB entry left with no path goes; a declaration left with no entry becomes `null`, no file.
 * Uncertain YAML, which certifies no ownership, is returned unchanged. */
export function liftStarterOwnership(text) {
  if (text === null || text === undefined || !ownedPatterns(text).length) return { legacy: [], text };
  const eol = text.includes('\r\n') ? '\r\n' : '\n';
  const kept = [];
  const legacy = [];
  let block = null;
  const flush = () => { if (block && (block.owner !== STARTER_OWNER || block.paths > 0)) kept.push(...block.lines); block = null; };
  for (const line of text.split(/\r?\n/)) {
    const owner = line.match(/^ *- +owner:[ \t]*(.*)$/);
    if (owner) { flush(); block = { owner: scalar(owner[1]), lines: [line], paths: 0 }; continue; }
    if (!block) { kept.push(line); continue; }
    const starter = block.owner === STARTER_OWNER;
    const item = line.match(/^ +- (.*)$/);
    const flow = line.match(/^( +paths:[ \t]*\[)([^\]]*)(\].*)$/);
    if (item) {
      const value = scalar(item[1]);
      if (starter && STARTER_PATHS.includes(value)) { legacy.push(value); continue; }
      block.paths++;
    } else if (flow) {
      const entries = flow[2].split(',').map(s => s.trim()).filter(Boolean);
      const remaining = entries.filter(entry => {
        if (starter && STARTER_PATHS.includes(scalar(entry))) { legacy.push(scalar(entry)); return false; }
        return true;
      });
      block.paths += remaining.length;
      if (remaining.length !== entries.length) { block.lines.push(`${flow[1]}${remaining.join(', ')}${flow[3]}`); continue; }
    }
    block.lines.push(line);
  }
  flush();
  if (!legacy.length) return { legacy, text };
  const result = kept.join(eol);
  return { legacy, text: /^ *- +owner:/m.test(result) ? result : null };
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

const SELECT = "Select a product with `rzm ao-kb refresh-bindings --product <slug>`, or `--single true` for a single-product KB.";

export function bindingRows(root, report) {
  const rows = [];
  const add = (name, status, message, path) => rows.push({ name, status, message, path });
  const codes = (report.diagnostics ?? []).map(d => d.code);
  const detail = (report.diagnostics ?? []).filter(d => d.severity === 'error').map(d => d.message || d.code).join(' ');
  // The compiler's state names what failed; a missing dependency or duplicate IDs is never reported as stale.
  const dependency = report.status === 'dependency missing' || codes.includes('ao_dependency_missing');
  const knowledgeRow = () => add('knowledge admission', report.knowledgeReady ? 'ok' : 'pending', report.knowledgeReady
    ? 'The generated knowledge admission matches the persisted product selection.'
    : `Knowledge typing is unscoped or stale until setup completes. ${SELECT}`, `${MODULE}/ontology/ao-knowledge.generated.graphql`);

  // Below schemaVersion 8 the model stage does not run, so no model row reports a dependency or readiness.
  const model = report.bindings?.model;
  if (model?.status === 'not applicable') {
    for (const [name, path] of [['model compiler dependencies', `${MODULE}/bindings`], ['model bindings', `${MODULE}/bindings/manifest.json`]]) {
      add(name, 'not applicable', model.reason, path);
    }
    add('model index', report.indexReady ? 'ok' : 'pending', report.indexReady
      ? 'A knowledge index receipt matches the knowledge bindings and the site sources.'
      : 'No knowledge index receipt for the current bindings and site sources; `node .rhizome/modules/ao-kb/bindings/cli.mjs index` indexes and writes one.', `${MODULE}/bindings/index-receipt.json`);
    knowledgeRow();
    return rows;
  }

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

  knowledgeRow();
  return rows;
}
