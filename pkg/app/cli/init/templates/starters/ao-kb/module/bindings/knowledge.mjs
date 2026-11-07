import {readFile, readdir, realpath} from 'node:fs/promises';
import path from 'node:path';
import {hash, json, readJSON, schemaFiles} from './runtime.mjs';
import {globMatch, ownedPatterns} from '../scripts/lib/bindings-check.mjs';

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
    const block = text.match(/^products:[ \t]*\r?\n((?:[ \t]*-[^\n]*\n)*)/m)?.[1] || '';
    return block.split('\n').filter(s => s.trim()).map(s => scalar(s.replace(/^\s*-\s*/, 'slug: '), 'slug'));
  }
  if (!value.startsWith('[') || !value.endsWith(']')) throw new Error('ao_product_invalid: products must be a scalar list');
  return value.slice(1, -1).split(',').filter(s => s.trim()).map(s => scalar(`slug: ${s.trim()}`, 'slug'));
}
async function text(root, name) {
  try { return await readFile(path.join(root, name), 'utf8'); }
  catch (error) { if (error.code === 'ENOENT') return null; throw error; }
}
export async function ownership(root, sources = []) {
  const patterns = ownedPatterns(await text(root, '.rhizome/source-ownership.yml'));
  const roots = ['assertions', 'sources', 'concepts', 'artifacts', '.kb/curation'];
  const unowned = roots.filter(dir => !['note.md', 'nested/note.md'].every(name => patterns.some(p => globMatch(p, `${dir}/${name}`)))).map(dir => `${dir}/**`);
  unowned.push(...sources.filter(name => !patterns.some(p => globMatch(p, name))));
  if (unowned.length) throw new Error(`ao_ownership_missing: declare these AO paths in .rhizome/source-ownership.yml before refresh: ${[...new Set(unowned)].join(', ')}`);
}
export async function knowledge(root, directory, selection) {
  const config = await text(root, '.kb/config/vault.md');
  if (config === null) throw new Error('ao_root_missing: expected .kb/config/vault.md at the AO account root');
  const fm = config.match(/^---\r?\n([\s\S]*?)\r?\n---/)?.[1] || '';
  if (scalar(fm, 'schemaVersion') !== '8') throw new Error('ao_schema_unsupported: schemaVersion 8 is required');
  const mode = scalar(fm, 'productMode') || 'single';
  const registered = registry(fm);
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
    const sources = [pin.source, 'git+ssh://git@github.com/atomicobject/atomic-ai-prompt-toolkit'];
    if (!engine || ![pin.version, pin.revision].includes(engine.version) || !sources.includes((engine.source || pin.source).replace(/\.git$/, ''))) throw new Error(`ao_toolkit_pin: ${name} must pin ${pin.version} (${pin.revision})`);
    inputs.push({path: name, hash: hash(bytes)});
  }
  await ownership(root);
  let phases = [];
  try { phases = (await readdir(path.join(root, 'artifacts'), {withFileTypes: true})).filter(e => e.isDirectory() && !registered.includes(e.name) && !e.name.startsWith('_')).map(e => e.name).sort(); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  if (phases.some(name => !/^[A-Za-z0-9_.-]+$/.test(name))) throw new Error('ao_product_path_unsupported: artifact phase directory names must be matcher path atoms');
  const lines = ['# Generated AO KB knowledge admission. Static knowledge types remain installed.'];
  if (product) for (const [type, dir] of [['Assertion', 'assertions'], ['Source', 'sources'], ['Concept', 'concepts'], ['Artifact', 'artifacts']]) {
    const locations = dir === 'artifacts' ? ['', ...phases.map(p => `${p}/`)] : [''];
    const selected = locations.map(p => `${dir}/${p}${product}`);
    const excluded = locations.flatMap(p => registered.map(s => `${dir}/${p}${s}`));
    const shared = `${dir} AND ${excluded.map(p => `NOT ${p}`).join(' AND ')} AND (products:${JSON.stringify(product)} OR products:"*" OR NOT has:products)`;
    lines.push(`extend type ${type} @admit(matches: ${JSON.stringify([...selected, shared])})`);
  }
  const sdl = lines.join('\n\n') + '\n';
  for (const name of ['.rhizome/source-ownership.yml', '.rhizome/config.yml']) {
    const bytes = await text(root, name);
    inputs.push({path: name, hash: bytes === null ? null : hash(bytes)});
  }
  for (const file of await schemaFiles(root)) inputs.push({path: path.relative(root, file), hash: hash(await readFile(file))});
  const compiler = {};
  for (const file of ['knowledge.mjs', 'runtime.mjs', 'generation.mjs', 'toolkit-pin.json', '../scripts/lib/bindings-check.mjs']) compiler[file] = hash(await readFile(path.join(directory, file)));
  return {sdl, context: {accountRoot: '.', product: product || null, productRoot: product ? `products/${product}` : '.'},
    fingerprint: hash(json({inputs, compiler, selection, phases, root})), inputs};
}
