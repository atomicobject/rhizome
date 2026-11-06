import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, symlink, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { readEvidence } from '../../templates/starters/ao-kb/module/scripts/read-evidence.mjs';
import { readPassage, readVaultText } from '../../templates/starters/ao-kb/module/views/knowledge/lib/passage.ts';
test('composition retains selector slots and exact context',async()=>{
 const source={path:'sources/s.md',body:'# Fallback\nbody',paths:['https://example.com/raw','missing.txt','sources/_raw/t.txt']};
 const read=async p=>p.endsWith('/t.txt')?'zero\none\ntwo\nthree\nfour\n':undefined;
 assert.equal((await readPassage(source,'2::L1',undefined,read)).status,'unresolved');
 const passage=await readPassage(source,'3::L2-L3',undefined,read);
 assert.deepEqual(passage,{status:'resolved',path:'sources/_raw/t.txt',startLine:2,endLine:3,lines:['one','two'],before:['zero'],after:['three','four'],hashStatus:'Passage hash not verified by Rhizome'});
 assert.equal((await readPassage(source,'L1',undefined,async()=>undefined)).lines[0],'# Fallback');
});
test('action reads bytes faithfully and confines real paths',async()=>{
 const root=await mkdtemp(join(tmpdir(),'ao-evidence-'));
 try {
  await mkdir(join(root,'sources/_raw'),{recursive:true});await mkdir(join(root,'.kb/index'),{recursive:true});
  const text='\ufeff---\r\nid: raw\r\n---\r\n# Exact\r\n';await writeFile(join(root,'sources/_raw/raw.md'),text);
  await writeFile(join(root,'.kb/index/kb-weights.json'),'{}');
  assert.deepEqual(await readEvidence(root,'sources/_raw/raw.md'),{content:text});
  assert.deepEqual(await readEvidence(root,'.kb/index/kb-weights.json'),{content:'{}'});
  await symlink('/etc/hosts',join(root,'sources/_raw/escape.txt'));
  for(const path of ['../outside','/etc/hosts','https://example.com','sources/source.md','sources/_raw/escape.txt','sources/_raw/missing.txt'])await assert.rejects(readEvidence(root,path));
 } finally {await rm(root,{recursive:true,force:true});}
});
test('browser access rejects unsafe paths and handles action envelopes',async()=>{
 const original=globalThis.fetch;const calls=[];
 try {
  globalThis.fetch=async(url,options)=>{calls.push([url,options]);return url.startsWith('/api/v1/files')?new Response('{}',{status:400}):Response.json({status:'ok',result:{content:'saved'}});};
  assert.equal(await readVaultText('../no'),undefined);assert.equal(calls.length,0);
  assert.equal(await readVaultText('.kb/index/kb-weights.json'),'saved');assert.equal(calls.length,2);
  globalThis.fetch=async()=>Response.json({status:'refused',reason:'untrusted'});
  await assert.rejects(readVaultText('.kb/index/kb-weights.json'));
  globalThis.fetch=async()=>{throw new Error('offline');};await assert.rejects(readVaultText('sources/_raw/raw.md'));
 } finally {globalThis.fetch=original;}
});

test('refused default file never becomes a fallback excerpt; selected files read alone',async()=>{
 const source={path:'sources/s.md',paths:['sources/_raw/a.txt','sources/_raw/b.txt'],body:'fallback'};
 assert.equal((await readPassage(source,'L1',undefined,async()=>{throw new Error('refused');})).status,'unresolved');
 const seen=[];const result=await readPassage(source,'2::L1',undefined,async p=>{seen.push(p);return 'selected';});
 assert.deepEqual(seen,['sources/_raw/b.txt']);assert.deepEqual(result.lines,['selected']);
});
