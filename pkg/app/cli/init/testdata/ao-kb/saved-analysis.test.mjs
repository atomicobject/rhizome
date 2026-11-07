import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseSavedAnalysis, savedAssertion, readSavedAnalysis } from '../../templates/starters/ao-kb/module/views/knowledge/lib/saved-analysis.ts';
// AO e9dce951: claim_weight.py SCRATCH --build-index --as-of 2026-11-01 --out weights.json
const sample = {
  "artifacts": {},
  "assertions": {
    "CL-1": {
      "band": "firm",
      "basis": "firm · stated directly · 0 sources · 0 days · unreviewed · fields predate 2.33",
      "inputs": {
        "ageDays": 0,
        "attestation": "direct",
        "attestationDefaulted": true,
        "attestationInvalid": false,
        "contradiction": "none",
        "contradictionIds": [],
        "dependents": 0,
        "independentSources": 0,
        "kind": "fact",
        "makerOnly": false,
        "origin": "",
        "originSet": [],
        "reviewed": false,
        "sourceCount": 0
      },
      "slug": "claim",
      "stored": "hypothesis",
      "weight": 1.0
    }
  },
  "concepts": {},
  "meta": {
    "asOf": "2026-11-01",
    "builder": "ao-kb@7.138.8",
    "config": {
      "weight_age_floor": 0.5,
      "weight_attestation_direct": 1.0,
      "weight_attestation_implied": 0.6,
      "weight_attestation_inferred": 0.4,
      "weight_attestation_secondhand": 0.5,
      "weight_band_firm": 0.8,
      "weight_band_leaning": 0.55,
      "weight_contradiction_blocking": -0.3,
      "weight_contradiction_sideline": -0.15,
      "weight_corroboration_2": 0.15,
      "weight_corroboration_3plus": 0.25,
      "weight_half_life_days_decision": 720,
      "weight_half_life_days_fact": 365,
      "weight_half_life_days_goal": 365,
      "weight_half_life_days_hypothesis": 180,
      "weight_half_life_days_need": 365,
      "weight_half_life_days_observation": 120,
      "weight_half_life_days_other": 365,
      "weight_half_life_days_preference": 540,
      "weight_half_life_days_risk": 180,
      "weight_half_life_days_rule": 540,
      "weight_independence_per_role_class": 0,
      "weight_kind_open": 0.4,
      "weight_kind_primary": 1.0,
      "weight_kind_secondary": 0.75,
      "weight_origin_end_user": 1.0,
      "weight_origin_external": 0.75,
      "weight_origin_generated": 0.7,
      "weight_origin_stakeholder": 1.0,
      "weight_origin_team_other": 0.75,
      "weight_origin_team_own": 1.0,
      "weight_origin_unrecorded": 1.0,
      "weight_penalty_per_contradiction": 1,
      "weight_review_bump": 0.15,
      "weight_review_half_life_days": 180
    },
    "generatedAt": "2026-11-01"
  }
};
test('AO generated snapshot stays verbatim and never implies freshness',()=>{
 const snapshot=parseSavedAnalysis(JSON.stringify(sample));assert.ok(snapshot);
 const assessment=savedAssertion(snapshot,'CL-1');
 assert.deepEqual(assessment,{status:'available',band:sample.assertions['CL-1'].band,basis:sample.assertions['CL-1'].basis,asOf:'2026-11-01',generatedAt:'2026-11-01',builder:sample.meta.builder});
 assert.equal(savedAssertion(snapshot,'CL-1','2026-11-02').mayBeStale,true);
 for(const date of [undefined,'unknown','2027-02-30','2026-10-31','2026-11-01'])assert.equal('mayBeStale' in savedAssertion(snapshot,'CL-1',date),false);
 assert.equal(savedAssertion(snapshot,'absent').status,'not-in-snapshot');
 assert.equal(savedAssertion(snapshot,'toString').status,'not-in-snapshot');
 assert.equal(savedAssertion(undefined,'CL-1').status,'unavailable');
 assert.equal(snapshot.assertions['CL-1'].stored,'hypothesis');
});
test('missing and malformed snapshots stay unavailable',async()=>{
 for(const input of [undefined,'not JSON','{}','null','[]',JSON.stringify({...sample,meta:{asOf:42}}),JSON.stringify({...sample,assertions:{bad:{band:'firm'}}})])assert.equal(parseSavedAnalysis(input),undefined);
 // The view's query resolves this value; TanStack Query treats undefined data as an error, so
 // an absent or unreadable snapshot must resolve null to render as unavailable (a real KB without kb-weights.json).
 assert.equal(await readSavedAnalysis(async()=>{throw new Error('unreadable');}),null);
 assert.equal(await readSavedAnalysis(async()=>undefined),null);
 assert.equal(await readSavedAnalysis(async()=>'not JSON'),null);
 let path;assert.ok(await readSavedAnalysis(async p=>{path=p;return JSON.stringify(sample);}));assert.equal(path,'.kb/index/kb-weights.json');
});
