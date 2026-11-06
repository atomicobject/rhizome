import test from 'node:test';
import assert from 'node:assert/strict';
import { canonicalEdges, makeResolver, normalizeRel, traversal } from '../../templates/starters/ao-kb/module/views/knowledge/lib/relations.ts';

test('normalizes spellings and inverse aliases like AO, keeping corroboration its own relation', () => {
  assert.deepEqual(normalizeRel('dependsOn'), { rel: 'depends-on', invert: false });
  assert.deepEqual(normalizeRel('depends_on'), { rel: 'depends-on', invert: false });
  assert.deepEqual(normalizeRel('supportedBy'), { rel: 'supports', invert: true });
  assert.deepEqual(normalizeRel('frontmatterSupersededBy'), { rel: 'supersedes', invert: true });
  assert.deepEqual(normalizeRel('blocks'), { rel: 'depends-on', invert: true });
  assert.deepEqual(normalizeRel('addressedBy'), { rel: 'resolves', invert: true });
  assert.deepEqual(normalizeRel('corroboratedBy'), { rel: 'corroborated-by', invert: false });
  assert.deepEqual(normalizeRel('mitigates'), { rel: 'mitigates', invert: false });
});

test('inverse declarations swap direction and body plus frontmatter duplicates count once', () => {
  const { edges, dangling } = canonicalEdges([
    { from: 'a.md', field: 'supports', to: 'b.md', origin: 'body' },
    { from: 'a.md', field: 'frontmatterSupports', to: 'b.md', raw: 'a-b', origin: 'frontmatter' },
    { from: 'c.md', field: 'supportedBy', to: 'a.md', origin: 'body' },
    { from: 'c.md', field: 'supersedes', to: 'c.md', origin: 'body' },
    { from: 'c.md', field: 'frontmatterRefines', to: undefined, raw: 'a-missing', origin: 'frontmatter' },
  ]);
  assert.equal(edges.length, 2);
  const ab = edges.find((e) => e.from === 'a.md' && e.to === 'b.md');
  assert.equal(ab.rel, 'supports');
  assert.deepEqual(ab.declaredBy.map((d) => d.origin), ['body', 'frontmatter']);
  const ac = edges.find((e) => e.from === 'a.md' && e.to === 'c.md');
  assert.equal(ac.rel, 'supports', 'c declared supported-by a, so a supports c');
  assert.deepEqual(ac.declaredBy, [{ record: 'c.md', as: 'supported-by', origin: 'body' }]);
  assert.deepEqual(dangling, [{ from: 'c.md', as: 'refines', raw: 'a-missing', origin: 'frontmatter' }]);
});

test('resolves AO ids before slugs and never picks a winner on ambiguity', () => {
  const resolve = makeResolver([
    { key: 'assertions/patients-book.md', aoId: 'a-000000000001', notePath: 'assertions/patients-book.md' },
    { key: 'sources/ops-sync.md', aoId: 's-000000000001', notePath: 'sources/ops-sync.md' },
    { key: 'sources/dup.md', aoId: 's-dup', notePath: 'sources/dup.md' },
    { key: 'sources/b/dup.md', aoId: 's-dup', notePath: 'sources/b/dup.md' },
  ]);
  assert.equal(resolve('a-000000000001'), 'assertions/patients-book.md');
  assert.equal(resolve('[[sources/ops-sync|Operations sync]]'), 'sources/ops-sync.md');
  assert.equal(resolve('Ops-Sync'), 'sources/ops-sync.md');
  assert.equal(resolve('s-dup'), undefined);
  assert.equal(resolve('dup'), undefined);
  assert.equal(resolve('a-missing'), undefined);
});

test('classifies traversal per the R7 table; other claim relations are list-only', () => {
  assert.equal(traversal('cites'), 'reference');
  assert.equal(traversal('corroborated-by'), 'reference');
  assert.equal(traversal('supersedes'), 'lateral');
  assert.equal(traversal('mentions'), 'lateral');
  assert.equal(traversal('extracted'), 'extraction');
  assert.equal(traversal('supports'), 'unclassified');
  assert.equal(traversal('depends-on'), 'unclassified');
});
