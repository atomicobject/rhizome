// The adapter owns AO semantics. This renderer owns the bounded RECORD spelling.
const quote = JSON.stringify;
export const typeName = name => `AOKBModel_${[...name].map(c => /[A-Za-z0-9]/.test(c) ? c : `_${c.codePointAt(0).toString(16)}_`).join('')}`;
const fieldName = name => `v_${[...name].map(c => /[A-Za-z0-9]/.test(c) ? c : `_${c.codePointAt(0).toString(16)}_`).join('')}`;
export const sourceType = (kind, origin) => `${typeName(kind)}_${origin}`;
const selectorPattern = /^(?:[A-Za-z_][A-Za-z0-9_-]*(?:\{\}\[\]|\[\]|\{\})?)(?:\.[A-Za-z_][A-Za-z0-9_-]*(?:\{\}\[\]|\[\]|\{\})?)*$/;
function selector(text) {
  if (typeof text !== 'string' || (text !== '' && !selectorPattern.test(text))) throw new Error(`record_selector_unsupported: ${text}`);
  return quote(text);
}
function keyPolicy(kind, kinds) {
  const offset = kind.container ? kinds.get(kind.container)?.selector.split('.').length || 0 : 0;
  const parts = kind.selector.split('.').slice(offset);
  if (parts.some(p => p.includes('{}')) && !parts.at(-1).endsWith('{}[]')) return '{mappingKey: true}';
  const key = kind.declaration.key;
  if (key == null) return 'null';
  if (typeof key !== 'string') throw new Error(`ao_key_unsupported: ${kind.name}`);
  if (!key.includes('{')) {
    if (!/^[A-Za-z_][A-Za-z0-9_-]*$/.test(key)) throw new Error(`ao_key_unsupported: ${key}`);
    return `{field: ${quote(key)}}`;
  }
  const stripped = key.replace(/\{\{|\}\}|\{[A-Za-z_][A-Za-z0-9_-]*\}/g, '');
  if (/[{}]/.test(stripped)) throw new Error(`ao_key_template_unsupported: ${key}`);
  return `{template: ${quote(key)}}`;
}
function edge(name, reverse) {
  if (!name) return '';
  const forward = Object.entries(reverse).find(([, backward]) => backward === name)?.[0];
  return `, edge: {name: ${quote(forward || name)}, direction: ${forward ? 'INBOUND' : 'OUTBOUND'}}`;
}
function fields(kind, ir, kinds, diagnostics) {
  const lines = ['  aoValue: String @field(path: "id")'];
  function add(path, fd, flattened = false) {
    selector(path);
    if (!fd || typeof fd !== 'object' || typeof fd.type !== 'string') throw new Error(`ao_field_unsupported: ${kind.name}.${path}`);
    const list = fd.type?.endsWith('[]');
    let scalar = ({int: 'Int', number: 'Float', bool: 'Boolean'})[fd.type] || 'String';
    const known = ['text', 'text[]', 'int', 'number', 'bool', 'date', 'enum', 'ref', 'ref[]', 'parent', 'typed-ref[]', 'variants'];
    const complex = !known.includes(fd.type);
    if (complex && !fd.to && !fd.to_kinds) {
      diagnostics.push({code: 'ao_inspection_only_field', severity: 'note', kind: kind.name, field: path,
        message: `${fd.type} remains available as native source content`});
    } else {
      let refs = '';
      if (fd.to || fd.type === 'typed-ref[]') {
        let names = fd.to ? [fd.to] : fd.to_kinds;
        const cfg = ir.referenceVocabulary.typed;
        names = names.map(n => n === cfg.pseudo_kind ? cfg.pseudo_target : n);
        const missing = names.filter(n => !kinds.has(n));
        if (missing.length) throw new Error(`ao_reference_target_unbound: ${kind.name}.${path}: ${missing.join(', ')}`);
        const external = names.some(n => kinds.get(n).declaration.file?.startsWith('kb:'));
        if (external) {
          if (names.some(n => kinds.get(n).declaration.file !== 'kb:assertions')) throw new Error(`ao_knowledge_target_unsupported: ${kind.name}.${path}: ${names.join(', ')}`);
          refs = ` @references(to: ["Assertion"], targetKey: "aoId", mode: STRICT, values: ${list ? 'ELEMENTS' : 'VALUE'}, allowWildcard: false${edge(fd.edge, ir.referenceVocabulary.reverse)})`;
        } else {
          const targets = [...new Set(names.map(typeName))];
          const mode = ['text', 'text[]'].includes(fd.type) ? 'TOLERANT' : 'STRICT';
          const values = list ? 'ELEMENTS' : fd.type === 'variants' ? 'MAPPING_KEYS' : complex ? 'BY_SHAPE' : 'VALUE';
          refs = ` @references(to: ${quote(targets)}, mode: ${mode}, values: ${values}, allowWildcard: ${fd.type !== 'typed-ref[]'}`;
          if (fd.match_name) refs += ', matchName: "name"';
          if (fd.type === 'typed-ref[]') {
            const tags = fd.to_kinds.map(tag => {
              const pseudo = tag === cfg.pseudo_kind;
              return `{tag: ${quote(tag)}, type: ${quote(typeName(pseudo ? cfg.pseudo_target : tag))}${pseudo ? ', memberPath: "fields[].name"' : ''}}`;
            });
            refs += `, encoding: TYPED, targets: [${tags.join(', ')}]`;
          }
          refs += edge(fd.type === 'parent' ? 'part-of' : fd.edge, ir.referenceVocabulary.reverse) + ')';
        }
      }
      if (flattened || list || fd.type === 'variants') scalar = `[${scalar}!]`;
      const fallback = path === 'group' && kind.selector.endsWith('{}[]') ? ', fallback: MAPPING_KEY' : '';
      lines.push(`  ${fieldName(path)}: ${scalar} @field(path: ${quote(path)}${fallback})${refs}`);
      if (fd.type === 'date' || fd.type === 'enum' || fd.required || fd.min != null || fd.max != null) {
        diagnostics.push({code: 'ao_validation_metadata', severity: 'note', kind: kind.name, field: path,
          message: 'AO validation declaration retained in the intermediate; this binding exposes the authored value'});
      }
    }
    for (const [name, sub] of Object.entries(fd.items || {}).sort(([a], [b]) => a.localeCompare(b))) add(`${path}[].${name}`, sub, true);
  }
  for (const [name, fd] of Object.entries(kind.fields).sort(([a], [b]) => a.localeCompare(b))) add(name, fd);
  if (kind.selector.endsWith('{}[]') && !Object.hasOwn(kind.fields, 'group')) lines.push('  aoGroup: String @field(path: "group", fallback: MAPPING_KEY)');
  return lines;
}
export function render(ir) {
  if (ir.formatVersion !== 1 || ir.compilerVersion !== '1' || ir.providerContract !== 'RECORD-1') throw new Error('ao_intermediate_version: expected compiler/RECORD contract 1');
  const diagnostics = [...ir.diagnostics];
  if (diagnostics.some(d => d.severity === 'error')) throw Object.assign(new Error('ao_compatibility_error: see diagnostics'), {diagnostics});
  const kinds = new Map(ir.kinds.map(k => [k.name, k]));
  const sources = new Map(ir.sources.map(s => [s.id, s]));
  const lines = ['# Generated by ao-kb bindings compiler 1. Refresh through the starter action.',
    'interface AOKBModelRecord @display(singular: "Model record", plural: "Model records", group: "Product model") { aoValue: String }'];
  for (const kind of [...kinds.values()].sort((a, b) => a.name.localeCompare(b.name))) {
    // One interface admits both physical origins for typed and ordinary reference lookup.
    lines.push(`interface ${typeName(kind.name)} { aoValue: String }`);
    if (!kind.sources.length) continue;
    const body = fields(kind, ir, kinds, diagnostics);
    for (const sid of kind.sources) {
      const source = sources.get(sid);
      const origin = source.compositionOrigin;
      const args = [`shape: RECORD`, `paths: [${quote(source.path)}]`, `select: ${selector(kind.selector)}`,
        `key: ${keyPolicy(kind, kinds)}`, 'title: ["name", "t", "term", "$key"]',
        `scalarAs: ${quote(typeof kind.declaration.key === 'string' && !kind.declaration.key.includes('{') ? kind.declaration.key : 'id')}`,
        `space: ${quote(`ao:${ir.context.product || 'single'}`)}`, `logicalKind: ${quote(kind.name)}`, `sourceOrder: ${source.sourceOrder}`];
      if (source.membership) args.push(`membership: {path: "products", includes: ${quote(source.membership.includes)}, includeMissing: false, wildcard: "*"}`);
      if (kind.container) {
        const parent = kinds.get(kind.container);
        if (!parent || !parent.sources.some(id => sources.get(id).compositionOrigin === origin)) throw new Error(`ao_container_unbound: ${kind.name} ${origin}`);
        args.push(`container: ${quote(sourceType(kind.container, origin))}`);
      }
      const childFields = [];
      for (const child of kinds.values()) {
        if (child.container !== kind.name || !child.sources.some(id => sources.get(id).compositionOrigin === origin)) continue;
        const count = kind.selector.split('.').length;
        const suffix = child.selector.split('.').slice(count).join('.');
        childFields.push(`  children_${fieldName(child.name)}: [${sourceType(child.name, origin)}!] @contains(shape: RECORD, select: ${selector(suffix)}, edge: {name: "made-of"})`);
      }
      lines.push(`type ${sourceType(kind.name, origin)} implements ${typeName(kind.name)} & AOKBModelRecord @node(locator: EMBEDDED, label: ${quote(kind.declaration.label || kind.name)})\n  @display(group: "Product model")\n  @source(${args.join(', ')}) {\n${[...body, ...childFields].join('\n')}\n}`);
    }
  }
  const paths = [...new Set(ir.sources.map(s => s.path))].sort();
  if (paths.length) lines.push(`type AOKBModelFile @node(locator: EMBEDDED, label: "Model source file")
  @source(shape: RECORD, paths: ${quote(paths)}, select: "", key: null, title: ["$key"], space: "ao-files", logicalKind: "model-file") {
  synced: String @field(path: "synced")
}`);
  const views = Object.fromEntries(ir.sources.map(source => {
    const type = sourceType(source.kind, source.compositionOrigin);
    return [`${type}.yaml`, `apiVersion: rhizome.view.v1\nid: ao-kb.model.${type}\nname: Model record\nsource: {kind: custom, entry: model.tsx}\nmount: {kind: node, type: ${type}, default: true}\n`];
  }));
  return {sdl: lines.join('\n\n') + '\n', views, diagnostics,
    typeMap: Object.fromEntries([...kinds].map(([name, kind]) => [name, {interface: typeName(name), types: kind.sources.map(id => sourceType(name, sources.get(id).compositionOrigin))}]))};
}
