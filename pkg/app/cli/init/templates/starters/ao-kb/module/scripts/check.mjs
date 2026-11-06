import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { pathToFileURL } from 'node:url';

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
function includes(text) {
  const notes = text?.match(/^notes:\s*\n((?:[ \t]+[^\n]*\n|\s*\n)*)/m)?.[1];
  const block = notes?.match(/^ +includes:\s*(\[[^\n]*\]|\n(?: +-[^\n]*\n)*)/m)?.[1];
  if (!block) return null;
  if (block.startsWith('[')) return block.slice(1, -1).split(',').map(v => v.trim().replace(/^(['"])(.*)\1$/, '$2'));
  return block.split('\n').filter(v => /^\s*-/.test(v)).map(v => v.replace(/^\s*-\s*/, '').replace(/\s+#.*$/, '').replace(/^(['"])(.*)\1$/, '$2'));
}

export function check(root) {
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
  add('product mode', mode === 'single' ? 'ok' : 'unsupported', mode === 'single'
    ? 'Single product supported.' : `Product mode ${mode || '(missing)'} needs product selection, arriving with model bindings; knowledge views meanwhile show all products.`, vaultPath);
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
    const covered = patterns && paths.every(path => patterns.some(p => ['**/*.md', '**', `${path}/**`, `${path}/**/*.md`].includes(p)))
      && patterns.some(p => ['**/*.md', '**', '.kb/**', '.kb/**/*.md', '.kb/curation/**', '.kb/curation/*.md', '.kb/curation/_conflicts.md'].includes(p));
    const hiddenRegistry = existsSync(join(root, '.kb/curation/_conflicts.md'));
    add('note includes', hiddenRegistry ? 'warning' : covered ? 'ok' : 'warning', hiddenRegistry
      ? 'Conflict registry exists under hidden .kb; current note discovery skips hidden directories even with includes. Registry projection needs shared discovery support. Preserve unrelated include rules.'
      : covered
      ? 'Simple includes cover AO Markdown paths, including .kb/curation/_conflicts.md. Ignore rules may still exclude files; prove projection after indexing.'
      : 'Coverage not established by simple includes. Inspect notes.includes and ignore rules; append required AO paths without replacing unrelated rules.', configPath);
  });
  // TODO(SPEC-0133 lane B): replace this diagnostic after source ownership admission lands.
  add('source ownership', 'pending', 'Lane B has not landed. AO-owned records are not protected from Rhizome writes; use AO KB skills to change knowledge.', module);
  inspect('knowledge bindings', `${module}/ontology`, () => {
    const dir = join(root, module, 'ontology');
    const installed = existsSync(dir) && readdirSync(dir).some(name => name.endsWith('.graphql'));
    add('knowledge bindings', installed ? 'ok' : 'error', installed
      ? 'Static Markdown knowledge bindings installed; no compiler required. Index and typed query still required.'
      : 'Static knowledge bindings missing; install the starter.', `${module}/ontology`);
  });
  add('product-model bindings', 'unsupported', 'Product-model YAML bindings and binding compiler/refresh are not available in this release.', module);
  const weightsPath = '.kb/index/kb-weights.json';
  inspect('saved analysis', weightsPath, () => {
    const weights = json(root, weightsPath);
    add('saved analysis', weights?.meta?.asOf ? 'ok' : 'warning', weights?.meta?.asOf
      ? `Saved AO analysis present, meta.asOf ${weights.meta.asOf}; freshness and judgments are not recomputed.`
      : 'Saved AO analysis unavailable (missing file or meta.asOf); this is not a clean analysis result.', weightsPath);
  });
  return { checks };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try { console.log(JSON.stringify(check(process.cwd()))); }
  catch (error) { console.log(JSON.stringify({ checks: [{ name: 'preflight', status: 'error', message: error.message, path: '.' }] })); process.exitCode = 1; }
}
