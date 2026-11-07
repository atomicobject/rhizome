import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { bindingRows, globMatch, runCompilerCheck } from './lib/bindings-check.mjs';

// Read only the scalar frontmatter keys and simple include lists used by this check.
function scalar(text, key) {
  const match = text.match(new RegExp(`^${key}:\\s*([^\\n]*)`, 'm'));
  return match?.[1].replace(/\s+#.*$/, '').trim().replace(/^(['"])(.*)\1$/, '$2');
}
function read(root, path) {
  try { return readFileSync(join(root, path), 'utf8'); }
  catch (error) { if (error.code === 'ENOENT') return null; throw error; }
}
function json(root, path) {
  const text = read(root, path);
  return text === null ? null : JSON.parse(text);
}
// Discovery treats an absent notes.includes as every Markdown file.
const DEFAULT_INCLUDES = ['**/*.md'];
function includes(text) {
  if (text !== null && !/^notes:/m.test(text)) return DEFAULT_INCLUDES;
  const notes = text?.match(/^notes:\s*\n((?:[ \t]+[^\n]*\n|\s*\n)*)/m)?.[1];
  if (notes !== undefined && !/^ +includes:/m.test(notes)) return DEFAULT_INCLUDES;
  const block = notes?.match(/^ +includes:\s*(\[[^\n]*\]|\n(?: +-[^\n]*\n)*)/m)?.[1];
  if (!block) return null;
  if (block.startsWith('[')) return block.slice(1, -1).split(',').map(v => v.trim().replace(/^(['"])(.*)\1$/, '$2'));
  return block.split('\n').filter(v => /^\s*-/.test(v)).map(v => v.replace(/^\s*-\s*/, '').replace(/\s+#.*$/, '').replace(/^(['"])(.*)\1$/, '$2'));
}

/** Knowledge prerequisites plus the binding compiler's freshness, as one report. Pass
 * `runCompiler` to replace the compiler's own check (tests do). */
export function check(root, { runCompiler = runCompilerCheck } = {}) {
  root = resolve(root);
  const checks = [];
  function add(name, status, message, path) { checks.push({ name, status, message, path }); }
  function inspect(name, path, fn) {
    try { fn(); } catch (error) { add(name, 'error', `Cannot read prerequisite: ${error.message}`, path); }
  }
  const module = '.rhizome/modules/ao-kb';
  add('starter installed', existsSync(join(root, module, 'actions/check.yaml')) ? 'ok' : 'error',
    'Static starter action installation; use init --check to verify all managed files.', `${module}/actions/check.yaml`);
  const vaultPath = '.kb/config/vault.md';
  const vault = read(root, vaultPath);
  add('AO KB root', vault === null ? 'error' : 'ok', vault === null
    ? 'AO KB must be at the vault root; missing .kb/config/vault.md.' : 'AO KB configuration found at the vault root.', vaultPath);
  const frontmatter = vault?.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/)?.[1] ?? '';
  const schema = scalar(frontmatter, 'schemaVersion');
  add('schemaVersion', schema === '8' ? 'ok' : 'unsupported', schema === '8'
    ? 'Inspected schemaVersion 8 baseline.' : `Unsupported schemaVersion ${schema || '(missing)'}; ${Number(schema) < 8 ? 'upgrade AO KB first' : 'requires a compatible knowledge adapter'}.`, vaultPath);
  const mode = scalar(frontmatter, 'productMode');
  add('product mode', mode === 'single' ? 'ok' : 'warning', mode === 'single'
    ? 'Single product supported.' : `Product mode ${mode || '(missing)'}: setup requires one explicit product for knowledge admission and model bindings. Before refresh, static knowledge typing is unscoped; run rzm ao-kb refresh-bindings --product <slug>.`, vaultPath);
  const stampPath = '.kb/config/manifest.json';
  inspect('toolkit stamp', stampPath, () => {
    const stamp = json(root, stampPath)?.toolkit;
    add('toolkit stamp', stamp?.version ? 'ok' : 'warning', stamp?.version
      ? `Last scaffold/sync: ao-kb ${stamp.version}${stamp.commit ? ` (${stamp.commit})` : ''}. Does not certify every file or current toolkit compatibility.`
      : 'No toolkit version recorded; template age unavailable.', stampPath);
  });
  const configPath = '.rhizome/config.yml';
  inspect('note includes', configPath, () => {
    const patterns = includes(read(root, configPath));
    const paths = ['assertions', 'sources', 'concepts', 'artifacts'];
    const knowledge = patterns && paths.every(path => patterns.some(p => ['**/*.md', '**', `${path}/**`, `${path}/**/*.md`].includes(p)));
    // Discovery skips hidden directories unless an include names the hidden path literally.
    const registry = !existsSync(join(root, '.kb/curation/_conflicts.md'))
      || patterns?.some(p => p === '.kb/curation/_conflicts.md' || p.startsWith('.kb/curation/') || p.startsWith('.kb/'));
    // Structured model sources need an include naming their extension; broad globs do not activate them.
    const model = (json(root, `${module}/bindings/manifest.json`)?.sources ?? []).map(s => s.path);
    const unincluded = [...new Set(model)].filter(path => !patterns?.some(p => p === path
      || (globMatch(p, path) && p.endsWith(path.slice(path.lastIndexOf('.')))))).sort();
    // A first includes list replaces the default, so it must restate it.
    const first = patterns === DEFAULT_INCLUDES ? ' notes.includes is absent, so discovery covers every Markdown file; start the new list with **/*.md so the rest of the repository stays indexed.' : '';
    add('note includes', knowledge && registry && !unincluded.length ? 'ok' : 'warning', knowledge && registry && !unincluded.length
      ? 'Includes cover the AO knowledge paths, the conflict registry, and the compiled model sources. Ignore rules may still exclude files; prove projection after indexing.'
      : knowledge && registry
      ? `Compiled model sources are not included, so they will not be indexed. Append these entries to notes.includes without replacing unrelated rules: ${unincluded.join(', ')}.${first}`
      : !knowledge
      ? 'Simple includes do not cover assertions/, sources/, concepts/, and artifacts/. Inspect notes.includes and ignore rules; append the missing AO paths without replacing unrelated rules.'
      : `Add the include .kb/curation/_conflicts.md: note discovery skips hidden directories unless an include names the path, so the conflict registry is not indexed.${first}`, configPath);
  });
  const ownershipPath = '.rhizome/source-ownership.yml';
  inspect('source ownership', ownershipPath, () => {
    const declared = new Set((read(root, ownershipPath) ?? '').split('\n')
      .map(line => line.match(/^\s*-\s*['"]?([^'"#\s]+)['"]?\s*(?:#.*)?$/)?.[1]).filter(Boolean));
    const required = ['.kb/**', 'assertions/**', 'sources/**', 'concepts/**', 'artifacts/**'];
    const missing = required.filter(path => !declared.has(path));
    add('source ownership', declared.size === 0 ? 'error' : missing.length ? 'warning' : 'ok', declared.size === 0
      ? 'No source ownership declaration: AO-owned records are editable through Rhizome. Re-run init with the ao-kb add-on or restore the file.'
      : missing.length
      ? `Ownership declaration does not list ${missing.join(', ')}; Rhizome can edit those AO records.`
      : 'AO KB knowledge paths are declared AO-owned; Rhizome refuses native edits to them.', ownershipPath);
  });
  inspect('knowledge bindings', `${module}/ontology`, () => {
    const dir = join(root, module, 'ontology');
    const installed = existsSync(dir) && readdirSync(dir).some(name => name.endsWith('.graphql'));
    add('knowledge bindings', installed ? 'ok' : 'error', installed
      ? 'Static Markdown knowledge schema installed. Refresh generates product admission; index and typed query still required.'
      : 'Static knowledge bindings missing; install the starter.', `${module}/ontology`);
  });
  inspect('frontmatter connections', 'assertions', () => {
    // A sequence under `connections:` ([{type, target}] or a bare id list) is not projected
    // in this release; map-shaped connections are. Name every affected file.
    const dirs = ['assertions', ...(existsSync(join(root, 'products')) ? readdirSync(join(root, 'products')).map(p => `products/${p}/assertions`) : [])];
    const affected = [];
    for (const dir of dirs) {
      if (!existsSync(join(root, dir))) continue;
      for (const name of readdirSync(join(root, dir), { recursive: true })) {
        if (!String(name).endsWith('.md')) continue;
        const path = `${dir}/${String(name).split('\\').join('/')}`;
        const fm = read(root, path)?.match(/^---\r?\n([\s\S]*?)\r?\n---/)?.[1] ?? '';
        if (/^connections:\s*\[/m.test(fm) || /^connections:\s*\r?\n\s+-/m.test(fm)) affected.push(path);
      }
    }
    add('frontmatter connections', affected.length ? 'unsupported' : 'ok', affected.length
      ? `${affected.length} assertion(s) declare frontmatter connections as a sequence, which this release does not project; their ## Connections bullets and map-shaped connections still are: ${affected.join(', ')}`
      : 'Frontmatter connections, where present, use the supported map shape.', 'assertions');
  });
  inspect('product tags', 'assertions', () => {
    // Admission matchers also read inline fields and compare case-insensitively; AO reads
    // frontmatter `products` exactly. Name the notes where the two could disagree.
    const slugs = frontmatter.match(/^products:\s*\[([^\]]*)\]/m)?.[1].split(',').map(s => s.trim().replace(/^(['"])(.*)\1$/, '$2')) ?? [];
    const affected = [];
    for (const dir of ['assertions', 'sources', 'concepts', 'artifacts']) {
      if (!existsSync(join(root, dir))) continue;
      for (const name of readdirSync(join(root, dir), { recursive: true })) {
        const path = `${dir}/${String(name).split('\\').join('/')}`;
        if (!path.endsWith('.md') || path.includes('/_raw/')) continue;
        const text = read(root, path) ?? '';
        const values = text.match(/^---\r?\n[\s\S]*?^products:\s*\[([^\]]*)\]/m)?.[1].split(',').map(s => s.trim().replace(/^(['"])(.*)\1$/, '$2')) ?? [];
        const loose = values.some(v => v !== '*' && !slugs.includes(v) && slugs.some(s => s.toLowerCase() === v.trim().toLowerCase()));
        if (loose || /^products::/im.test(text.replace(/^---\r?\n[\s\S]*?\r?\n---/, ''))) affected.push(path);
      }
    }
    add('product tags', affected.length ? 'unsupported' : 'ok', affected.length
      ? `${affected.length} note(s) tag products with an inline products:: field or a case/space variant of a registered slug. AO ignores these; Rhizome's product admission may not: ${affected.join(', ')}`
      : 'Knowledge product tags use exact registered slugs in frontmatter.', 'assertions');
  });
  const weightsPath = '.kb/index/kb-weights.json';
  inspect('saved analysis', weightsPath, () => {
    const weights = json(root, weightsPath);
    add('saved analysis', weights?.meta?.asOf ? 'ok' : 'warning', weights?.meta?.asOf
      ? `Saved AO analysis present, meta.asOf ${weights.meta.asOf}; freshness and judgments are not recomputed.`
      : 'Saved AO analysis unavailable (missing file or meta.asOf); this is not a clean analysis result.', weightsPath);
  });
  const bindings = runCompiler(root);
  checks.push(...bindingRows(root, bindings));
  return {
    checks,
    starterInstalled: checks.find(c => c.name === 'starter installed')?.status === 'ok',
    bindingsReady: Boolean(bindings.bindingsReady),
    knowledgeReady: Boolean(bindings.knowledgeReady),
    modelReady: Boolean(bindings.modelReady),
    indexReady: Boolean(bindings.indexReady),
    bindings: { status: bindings.status, bindingsReady: Boolean(bindings.bindingsReady), knowledge: bindings.bindings?.knowledge,
      model: bindings.bindings?.model, indexReady: Boolean(bindings.indexReady), dependencies: bindings.dependencies, diagnostics: bindings.diagnostics ?? [] },
  };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try { console.log(JSON.stringify(check(process.cwd()))); }
  catch (error) { console.log(JSON.stringify({ checks: [{ name: 'preflight', status: 'error', message: error.message, path: '.' }] })); process.exitCode = 1; }
}
