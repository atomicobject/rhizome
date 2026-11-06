import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolvePassage, splitLines, noteBody, sourceReference, safeLocalPath, HASH_STATUS } from '../../templates/starters/ao-kb/module/views/knowledge/lib/locators.ts';
const cases=JSON.parse(readFileSync(new URL('./locator-parity/cases.json',import.meta.url)));
const golden=JSON.parse(readFileSync(new URL('./locator-parity/golden.json',import.meta.url)));
assert.equal(golden.toolkitCommit,'e9dce951ab4cd6aa9ccc734df96590bde25ccde9');
for (const [i,c] of cases.entries()) test(`AO parity: ${c.name}`,()=>{
 const actual=resolvePassage({path:'sources/source.md',paths:c.paths,body:c.body??'',scheme:c.scheme},c.locator,new Map(Object.entries(c.files)));
 const expected=golden.results[i];
 if(c.name.startsWith('timestamp')) { assert.equal(actual.status,'unsupported');assert.equal(actual.scheme,'timestamp'); }
 else {assert.equal(actual.status,expected.status);if(actual.status==='resolved') {assert.deepEqual(actual.lines,expected.lines);assert.equal(actual.endLine-actual.startLine+1,actual.lines.length);}}
 assert.equal(actual.hashStatus,HASH_STATUS);
});
test('body, source precedence, unsupported refs and path boundaries',()=>{
 assert.equal(noteBody('\ufeff---\r\nid: s\r\n---\r\n\r\n# A\r\n'),'# A\n');
 assert.equal(sourceReference('## Sources\n[[sources/first.md|First]]\n[[second]]'),'first');
 assert.equal(sourceReference('## Sources\n[[first]]','explicit'),'explicit');
 assert.equal(resolvePassage(undefined,'L1',new Map()).reason,'unknown source');
 assert.equal(resolvePassage({path:'sources/s.md',paths:[],body:''},undefined,new Map()).status,'document-only');
 assert.equal(resolvePassage(undefined,'field',new Map(),'view:foo').status,'unsupported');
 for(const p of ['/a','../a','a/../b','https://a','C:\\a','\\server\\a'])assert.equal(safeLocalPath(p),false);
 assert.deepEqual(splitLines('a\n\n'),['a','']);
});
