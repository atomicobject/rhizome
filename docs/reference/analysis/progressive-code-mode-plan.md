---
type: ReferenceDoc
summary: "Narrowed delivery contract and evidence for selective discovery and a generated typed CLI client."
reference-kind: analysis
last-verified: 2026-09-07
status: active
---

# Progressive code mode implementation

## Current authority and scope

Drew's coordinator dispatch on 2026-09-07 superseded planning-only authority and directed implementation through verification and a merge-ready child PR. Existing-catalog ownership and thin CLI transport are accepted. [SPEC-0091](../../specs/technical/progressive-agent-code-mode.md) and the owned effort freeze the narrowed slice. No routine implementation approval is outstanding.

Deliver `file_context` and `files` only: runtime-free selective discovery plus explicitly generated ESM and concrete adjacent TypeScript declarations. Keep existing command output, warnings, refs, runtime effects and minimal startup. Do not introduce a persistent service, second operation registry, semantic writes, full-catalog migration, automatic init/index client generation, or a new `--read-mode` API.

The prior plan made passive indexed GraphQL a prerequisite and proposed nine model launches. Both are superseded. Indexed query mode is a separate follow-up if justified; A owns one later bounded evaluation manifest, and this effort launches no evaluated-model calls.

## Source findings and tradeoffs

Initial checkout exactly matched required ancestor `3b93489c4e404981fc4bb193e75c0d70e9eac7a7`. Delivery refreshed integration at `c534bfb8` and read the [research handoff](agent-experience-research-handoff.md).

- `pkg/app/agentapi/catalog.go` owns operation identity, handler factories and surface membership. Selected code contracts extend that metadata; they do not replace dispatch.
- `cmd/agent_surface.go` is the authoritative existing CLI surface. New code discovery is opt-in and plain `agent start` remains minimal.
- `pkg/app/agentapi/agentapi.go` preserves shared handler JSON normalization. The generated client invokes the existing CLI, so operation-specific one-shot runtime plans remain authoritative.
- `pkg/app/mcp/tool_context.go` allows link-target application in broader tool use. The pilot restricts it to `never` and does not infer safe arguments from tool-level mutation labels.
- `cmd/ontology_query_runtime.go` and `pkg/app/oneshotruntime/ontology_query.go` refresh current projection with live writer access. Wrapping existing coverage queries does not make them passive. Disposable evaluation fixtures may have index/cache/session side effects; record them rather than adding broad infrastructure first.
- `.rhizome/query-recipes/complex-domain.yaml` caps `coverage-gap-pack` at 200 rows. Structural trace presence is distinct from verified coverage; empty or capped results must remain qualified.
- `pkg/app/web/ontology_sessions.go` resolves preview identity through `PreviewWithRefLineage` under lock. Future semantic writes must preserve canonicalRef, lineage, restore fingerprints and replay conflicts. They are outside this slice.

PR #157 is source material, not a base: live planning inspection found it open/conflicting at `b1f971d7ef572179459c839eb0380d3da756afae`. The preparation assessment records it 92 main commits behind the assessed baseline. Its checked discovery fixture has 38 operations, one recipe, 164,332 bytes and 41,083 estimated tokens. Its older four-call experiment exposed 35,717 direct-call bytes versus 149,517 first-use code-mode bytes with similar wall time. Those are historical branch measurements, not current Luna benchmarks.

The branch's schema sharing, deterministic artifacts and explicit executable ideas inform this pilot. Its complete registry/service/init migration and its SPEC-0089 are not imported; current main already uses SPEC-0089 for a different technical contract.

| Alternative | Decision |
| --- | --- |
| Efficient ordinary CLI/GraphQL script | Mandatory strong baseline; already supports batching and local reduction. May remain preferable. |
| Small generated typed CLI client | Accepted pilot: validates/restricts input construction, exposes selected contracts and manages child lifetime. Count setup cost; no assumed latency win. |
| Native host/MCP programmatic tools | Optional only when actually available in the evaluated harness; label CLI-mediated direct operations honestly. |
| Persistent service / full SDK | Excluded until evidence demonstrates a need beyond the competent batched baseline. |

## API and effects

`agent code surface` advertises the small supported selection and its effects without schema/index/session initialization. `agent code describe --operation <name>` returns only complete selected contracts. `agent code generate --operation <name> --output <task-directory>` explicitly writes selected executable/declaration artifacts and returns a manifest. Repeat `--operation` for both operations. Empty/unknown selections fail; no implicit full discovery.

The client uses explicit absolute executable and vault paths, argument arrays without a shell, and the supplied session ID. It fixes file context link-target mode to `never` and files depth to zero, restricting exposed inputs to the contract. All results retain parsed CLI payload, exit status and stderr. `ok` reports the CLI exit status; callers must still inspect domain errors and warnings inside the preserved payload. Cancellation/deadline/output bounds and close reap owned children; no automatic retry, cross-call snapshot or transaction is implied.

Immutable hashed artifacts omit vault contents, credentials and session IDs. Manifest response may include current absolute executable/vault paths for use; those runtime values are excluded from deterministic contract identity. Generation is explicit and never happens during discovery, client creation, init or index. Client use rejects a stale selected contract instead of regenerating silently. Generated declarations describe real known output fields and preserve open extension values honestly.

Current handlers may touch existing session bookkeeping or index/cache state. This is disclosed rather than renamed as a passive runtime. Source note/code files must remain unchanged in deterministic and later model fixtures.

## Workloads for A

A owns the later manifest, model budget and execution; evaluated model is Luna xhigh only. No independent nine-launch matrix survives. Shared tasks and access:

| Task | Required evidence / equal access |
| --- | --- |
| Base-context gathering | Given two known code paths, gather applicable governing docs and exact selected content; preserve paths, warnings and omitted/truncated evidence. Direct CLI, efficient repeated-flag batching, and typed fileContext/files have equal operations. |
| Requirement coverage | Return accepted requirement coverage rows with id/path, status, source paths, specs, storyRefs and acceptanceCriterionRefs from existing `coverage-gap-pack`. Every arm may invoke the same existing CLI/GraphQL. Typed-client arm is explicitly hybrid here; no typed recipe function is claimed. Do not require unsupported requirement-to-code/test traversal. |
| Trivial lookup | Read one known small source note using the same existing files operation; direct route incurs no generated-client setup unless that arm chooses it. |

Arms: direct operation calls (CLI-mediated when shell is the transport), efficient batched CLI/GraphQL with host reduction, typed progressive client. Native host/MCP is optional and not a blocker. Every arm may create isolated writable task artifacts and use existing index/cache state; note/code source content is preserved. Count generation, discovery, script source, index/projection setup and runtime, stderr, retries and final evidence inclusively for first use, and report repeated use separately. Do not pre-generate invisibly. No model-benefit threshold is a product promise; A/coordinator settle the later bounded comparison.

Seed a current governing doc, historical supporting doc, an accepted untraced requirement, a covered accepted requirement, a candidate, and an invalid source/trace case. A must distinguish absent traces from valid targets and preserve the 200-row coverage bound. The live planning coverage pack was empty and does not validate this task.

## Runnable setup for A and B

From the source-built checkout, select only the operations needed for the task:

```sh
./scripts/rzm agent code surface
./scripts/rzm agent code describe --operation files
./scripts/rzm agent code generate --operation files --operation file_context --output /tmp/rhizome-code-task > /tmp/rhizome-code-manifest.json
```

Use the returned absolute paths; generation does not install a global module. A task script can consume the manifest as follows:

```js
import { readFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
const manifest = JSON.parse(await readFile("/tmp/rhizome-code-manifest.json", "utf8"));
const { createClient } = await import(pathToFileURL(manifest.modulePath).href);
const client = createClient({
  executablePath: manifest.executablePath,
  vaultPath: manifest.vaultPath,
});
try {
  const result = await client.files({
    inputs: ["docs/specs/technical/progressive-agent-code-mode.md"],
    includeContent: true,
    limit: 1,
  });
  console.log(JSON.stringify(result));
} finally {
  await client.close();
}
```

The direct equivalent is `./scripts/rzm agent files --input docs/specs/technical/progressive-agent-code-mode.md --include-content true --max-depth 0 --limit 1`. Multiple `--input` or `--file` flags form the competent batched baseline. A must replace this example path with its identical seeded fixture paths across arms and include setup plus the per-call live description check in measured costs.

Coordinator-owned release note proposal: “Add selective agent code discovery and explicitly generated typed CLI clients for file context and file reads.” B owns installed guidance; these examples document the verified implementation without changing generated skill copies.

## Ownership and verification

E owns new `pkg/app/agentcode/**`, selected contract metadata/tests in `pkg/app/agentapi/`, new `cmd/agent_code.go` plus agent registration and runtime-free registry entries, the new TechnicalSpec, effort and this note. Relevant existing subsystem/package docs receive only direct contract updates. B owns installed examples; A owns model runner/fixtures and the eventual manifest. Shared specs beyond the newly granted SPEC-0091 remain coordinator-owned. Original Complex Domain alignment/backport/compound/closure remains a coordinator prerequisite before changing its old spec/starter assets.

Delivery sequence: freeze actual scope and identity; implement selected catalog metadata/generator/client; wire runtime-free CLI; run real Node and CLI tests plus fault cases; review actual diff independently; fix current-head feedback and gates; update PR #247 to delivered scope. Coordinator merges into staging; E never merges integration/main.

Meaningful checks include selected-schema completeness/exclusion, empty/invalid selection, argument injection and unauthorized options, deterministic/atomic generation, stale contract before task calls, output/error preservation, deadline/cancellation/output bounds/concurrency/close, real CLI fixture source hashes and minimal startup. Package tests use vendored dependencies and `fts5`; full `make check` remains required before production commit. Markdown and effort changes run project-launcher validation and frozen-scope-drift. Source-built CLI verification prevents an installed older binary from masquerading as delivery evidence.
