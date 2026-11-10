import {readFile, readdir, realpath} from 'node:fs/promises';
import path from 'node:path';
import {hash, json, readJSON, schemaFiles} from './runtime.mjs';

// AO's registry/marker fields use scalars and scalar lists. Refuse YAML constructs
// outside that contract rather than interpreting aliases or nested objects.
export function scalar(text, key) {
  const value = text.match(new RegExp(`^${key}:[ \\t]*([^\\n\\r]*)`, 'm'))?.[1]?.replace(/\s+#.*$/, '').trim();
  if (value === undefined) return undefined;
  if (/^["']/.test(value)) {
    if (value[0] !== value.at(-1)) throw new Error(`ao_config_unsupported: ${key}`);
    return value.slice(1, -1);
  }
  if (/[\[\]{}&*!|>]/.test(value)) throw new Error(`ao_config_unsupported: ${key}`);
  return value;
}
function registry(text) {
  const value = text.match(/^products:[ \t]*([^\r\n]*)/m)?.[1]?.trim();
  if (!value) {
    // The captured frontmatter has no trailing newline, so the last item may end the text.
    const block = text.match(/^products:[ \t]*\r?\n((?:[ \t]*-[^\r\n]*(?:\r?\n|$))*)/m)?.[1] || '';
    return block.split('\n').filter(s => s.trim()).map(s => scalar(s.replace(/^\s*-\s*/, 'slug: '), 'slug'));
  }
  if (!value.startsWith('[') || !value.endsWith(']')) throw new Error('ao_product_invalid: products must be a scalar list');
  return value.slice(1, -1).split(',').filter(s => s.trim()).map(s => scalar(`slug: ${s.trim()}`, 'slug'));
}
// AO reads a vault.md that records no schemaVersion as current (metamodel.vault_schema_version).
export const schemaVersion = config => Number(config?.match(/^schemaVersion:[ \t]*['"]?(\d+)/m)?.[1] ?? 8);
// Product-model bindings need the current schema; knowledge reads 4 to 7 through the legacy spellings.
export const MODEL_SCHEMA = 8;
export const notApplicable = version => `Product-model bindings need AO schemaVersion ${MODEL_SCHEMA}; this KB records schemaVersion ${version}, so the model stage is not applicable. Knowledge reads through the legacy spelling adapter. Upgrade the KB with AO's own migration to enable the model.`;
const DIRS = ['assertions', 'sources', 'concepts', 'artifacts'];
async function text(root, name) {
  try { return await readFile(path.join(root, name), 'utf8'); }
  catch (error) { if (error.code === 'ENOENT') return null; throw error; }
}
export async function knowledge(root, directory, selection) {
  const config = await text(root, '.kb/config/vault.md');
  if (config === null) throw new Error('ao_root_missing: expected .kb/config/vault.md at the AO account root');
  const fm = config.match(/^---\r?\n([\s\S]*?)\r?\n---/)?.[1] || '';
  const version = schemaVersion(config);
  if (version < 4 || version > MODEL_SCHEMA) throw new Error(`ao_schema_unsupported: schemaVersion ${version}; knowledge supports schemaVersion 4 through ${MODEL_SCHEMA}`);
  const mode = scalar(fm, 'productMode') || 'single';
  const registered = registry(fm);
  if (version < MODEL_SCHEMA && (mode !== 'single' || selection?.product)) throw new Error(`ao_schema_unsupported: schemaVersion ${version} ${mode !== 'single' ? `in product mode ${mode}` : `with product ${selection.product} selected`}; legacy product-mode accounts are not supported. Upgrade the KB with AO's own migration`);
  if (registered.some(s => !/^[A-Za-z0-9_-]+$/.test(s)) || new Set(registered).size !== registered.length) throw new Error('ao_product_invalid: registry slugs must be unique simple directory names');
  const inputs = [{path: '.kb/config/vault.md', hash: hash(config)}];
  let entries = [];
  try { entries = await readdir(path.join(root, 'products'), {withFileTypes: true}); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  const marked = [];
  for (const slug of [...new Set([...registered, ...entries.filter(e => e.isDirectory()).map(e => e.name)])].sort()) {
    const name = `products/${slug}/.kb/config/product.yaml`;
    const marker = await text(root, name);
    inputs.push({path: name, hash: marker === null ? null : hash(marker)});
    if (marker === null) continue;
    if (!registered.includes(slug)) throw new Error(`ao_product_unregistered: ${slug}`);
    if (scalar(marker, 'slug') !== slug || await realpath(path.resolve(root, `products/${slug}`, scalar(marker, 'account') || '.')) !== root) throw new Error(`ao_product_invalid: ${name} disagrees with its account/slug`);
    marked.push(slug);
  }
  const product = selection?.product;
  if (!selection) throw new Error(`ao_selection_required: typing is unscoped until setup; run rzm ao-kb refresh-bindings ${marked.length || mode !== 'single' ? '--product <slug>' : '--single true'}`);
  if (marked.length && !product) throw new Error('ao_product_required: run rzm ao-kb refresh-bindings --product <slug>');
  if (!marked.length && mode !== 'single') throw new Error('ao_product_setup_incomplete: registry has no marked product roots');
  if (product && !marked.includes(product)) throw new Error(`ao_product_invalid: ${product} is not a registered, marked product`);
  const pin = await readJSON(path.join(directory, 'toolkit-pin.json'));
  for (const base of new Set(['', product ? `products/${product}/` : ''])) {
    const name = `${base}.kb/config/manifest.json`;
    const bytes = await text(root, name);
    const engine = bytes === null ? null : JSON.parse(bytes).siteEngine;
    // A single-product KB with no manifest, or a scaffold manifest without siteEngine (AO's older
    // scaffolds), never enabled AO's site engine, so no other reader is pinned. The manifest's
    // presence and bytes stay fingerprint inputs: enabling the engine later stales these bindings.
    if (!engine && !product) { inputs.push({path: name, hash: bytes === null ? null : hash(bytes)}); continue; }
    const sources = [pin.source, 'git+ssh://git@github.com/atomicobject/atomic-ai-prompt-toolkit'];
    if (!engine || ![pin.version, pin.revision].includes(engine.version) || !sources.includes((engine.source || pin.source).replace(/\.git$/, ''))) throw new Error(`ao_toolkit_pin: ${name} must pin ${pin.version} (${pin.revision})`);
    inputs.push({path: name, hash: hash(bytes)});
  }
  let phases = [];
  try { phases = (await readdir(path.join(root, 'artifacts'), {withFileTypes: true})).filter(e => e.isDirectory() && !registered.includes(e.name) && !e.name.startsWith('_')).map(e => e.name).sort(); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  if (!product) phases = [];
  if (phases.some(name => !/^[A-Za-z0-9_.-]+$/.test(name))) throw new Error('ao_product_path_unsupported: artifact phase directory names must be matcher path atoms');
  const lines = ['# Generated AO KB knowledge admission. Static knowledge types remain installed.'];
  if (product) for (const [type, dir] of [['Assertion', DIRS[0]], ['Source', DIRS[1]], ['Concept', DIRS[2]], ['Artifact', DIRS[3]]]) {
    const locations = dir === 'artifacts' ? ['', ...phases.map(p => `${p}/`)] : [''];
    const selected = locations.map(p => `${dir}/${p}${product}`);
    const excluded = locations.flatMap(p => registered.map(s => `${dir}/${p}${s}`));
    const shared = `${dir} AND ${excluded.map(p => `NOT ${p}`).join(' AND ')} AND (products:${JSON.stringify(product)} OR products:"*" OR NOT has:products)`;
    lines.push(`extend type ${type} @admit(matches: ${JSON.stringify([...selected, shared])})`);
  }
  // The documents select every products/*/ root; admission keeps the account root and the selected product's.
  if (product) for (const [type, account] of [['MapOfContent', ['_moc.md', 'MOCs']],
    ['Submission', ['_improvements AND has:status AND NOT _improvements/_archive AND NOT _improvements/_pending-attachments']],
    ['CurationDocument', ['.kb/curation']]]) {
    const scoped = type === 'Submission' ? [`products/${product}/_improvements AND has:status AND NOT products/${product}/_improvements/_archive AND NOT products/${product}/_improvements/_pending-attachments`] : [`products/${product}`];
    lines.push(`extend type ${type} @admit(matches: ${JSON.stringify([...account, ...scoped])})`);
  }
  const sdl = lines.join('\n\n') + '\n';
  // The declaration stays a fingerprint input: lifting or editing it asks for a refresh.
  for (const name of ['.rhizome/source-ownership.yml']) {
    const bytes = await text(root, name);
    inputs.push({path: name, hash: bytes === null ? null : hash(bytes)});
  }
  for (const file of await schemaFiles(root)) inputs.push({path: path.relative(root, file), hash: hash(await readFile(file))});
  const compiler = {};
  for (const file of ['knowledge.mjs', 'runtime.mjs', 'generation.mjs', 'toolkit-pin.json', '../scripts/lib/bindings-check.mjs']) compiler[file] = hash(await readFile(path.join(directory, file)));
  // The profile is only displayed, so a value the scalar reader refuses is omitted with a note.
  const notes = [];
  let profile;
  try { profile = scalar(fm, 'profile'); }
  catch { notes.push({code: 'ao_profile_unreadable', severity: 'note', message: 'ao_profile_unreadable: .kb/config/vault.md records a profile the scalar reader refuses; it is omitted from the knowledge context'}); }
  return {sdl, artifactPhases: phases, schemaVersion: version, notes,
    context: {accountRoot: '.', product: product || null, productRoot: product ? `products/${product}` : '.', ...(profile ? {profile} : {})},
    fingerprint: hash(json({inputs, compiler, selection, phases, root})), inputs};
}

/** Every file the site reads, by the starter's type and admission path rules (SPEC-0135 R2b),
 * at the account root and the selected product's root. Frontmatter conditions (`has:status`,
 * `products:`) are not reproduced, so this is a deliberate superset. */
export async function siteInventory(root, product = null) {
  const files = [];
  async function walk(dir, keep = () => true, depth = Infinity) {
    let entries;
    try { entries = await readdir(path.join(root, dir), {withFileTypes: true}); }
    catch (error) { if (error.code === 'ENOENT' || error.code === 'ENOTDIR') return; throw error; }
    for (const entry of entries) {
      const name = `${dir}/${entry.name}`;
      if (entry.isDirectory() && depth > 0 && keep(name)) await walk(name, keep, depth - 1);
      else if (entry.isFile() && entry.name.endsWith('.md') && keep(name)) files.push(name);
    }
  }
  for (const base of ['', ...(product ? [`products/${product}/`] : [])]) {
    for (const dir of DIRS) await walk(`${base}${dir}`, name => name !== `${base}sources/_raw`);
    await walk(`${base}MOCs`);
    await walk(`${base}_improvements`, name => ![`${base}_improvements/_archive`, `${base}_improvements/_pending-attachments`].includes(name));
    await walk(`${base}.kb/curation`, undefined, 0);
    if ((await text(root, `${base}_moc.md`)) !== null) files.push(`${base}_moc.md`);
  }
  return files.sort();
}

/** The index receipt's source signature: the model sources (schemaVersion 8) plus the site inventory. */
export async function indexSignature(root, ir, product = null) {
  const files = [...new Set([...(ir?.sources || []).map(s => s.path), ...await siteInventory(root, product)])].sort();
  const revisions = [];
  for (const name of files) {
    let revision = null;
    try { revision = hash(await readFile(path.join(root, name))); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    revisions.push([name, revision]);
  }
  return hash(json(revisions));
}
