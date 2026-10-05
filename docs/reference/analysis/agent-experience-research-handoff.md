---
type: ReferenceDoc
summary: "Assessment rationale, evidence limits, and supervisor handoff for the Rhizome agent experience program."
reference-kind: analysis
last-verified: 2026-09-07
status: draft
---

# Agent experience research handoff

## Read this with the effort

For current execution authority, supervisor IDs and later decisions, read the [delivery coordination record](agent-experience-delivery-coordination.md). It supersedes this handoff's earlier planning-only checkpoint and blanket original-Domain-closure dependency. The substantive evidence distinctions below remain applicable.

The [preparation brief](agent-experience-preparation.md) owns user decisions and the package map. The [integration effort](../../efforts/2026-09-05-11-42-starter-skill-consolidation.md) owns coordination and approval state. Each child effort owns its implementation plan and execution evidence. This note preserves the assessment's rationale and uncertainty so a new supervisor can challenge the conclusions without repeating the entire investigation.

Read the preparation brief, your own effort, and the relevant sections below. Then inspect the linked current sources for any contract your changes affect. Do not load every skill, graph neighborhood, or historical analysis by default. This note is assessment evidence and design reasoning, not authority to change an approved product contract.

Assessment context: initial source review used `8d01660f`; staging began at `b978cc53`; the five planning handoffs were pushed at `3b93489c`. Source paths below were checked in staging. Behavioral claims are observations from that assessment or explicit hypotheses, not proof that a redesign has shipped. Recheck runtime and PR state before relying on it.

## Supervisor and worker policy

Drew proposed this development-team allocation on 2026-09-07:

| Responsibility | Model and reasoning | Use |
| --- | --- | --- |
| Effort supervisor | `gpt-6-astra`, `low` | Retain intent, synthesize findings, plan, delegate, inspect changes, and own the child PR. |
| Targeted worker | `gpt-5.6-luna`, `max` | Bounded probes, explicit mechanical changes, fixture work, and well-specified tests. |
| Moderate implementation | `gpt-5.6-sol`, `medium` | Work needing several local decisions inside established contracts. |
| Subtle implementation | `gpt-6-astra`, `low` | Cross-cutting semantics, authority/freshness handling, identity, or runtime behavior needing careful judgment. |

This is an initial allocation policy, not a measured model ranking. Select by ambiguity and cost of a mistake, not file count. A small edit to identity or authorization can belong in the subtle lane. Start with a small worker set, usually one or two per active effort, and delegate only when the supervisor has useful independent work. Workers do not recursively fan out by default. Follow the available model/effort combinations; do not silently substitute unsupported settings.

The supervisor remains responsible for the result. It reviews the actual diff and observable evidence rather than forwarding a worker's confidence. Contract-sensitive changes should get a fresh independent review; increase reasoning for a specific unresolved issue when the ordinary pass cannot resolve it, rather than applying an expensive setting to every task.

**Evaluation is separate:** the model under test is `gpt-5.6-luna` at `xhigh`. No Astra or Sol evaluation runs are included. Luna `max` is the targeted development worker, not the evaluation configuration. Model runs still require the bounded evaluation protocol in the approved plan.

## What must survive the redesign

### 1. General use is a product experience, not just a command manual

**Observation:** the current [core skill](../../../pkg/app/cli/init/templates/skills/markdown/rhizome/SKILL.md) and [managed contract](../../rhizome-md-templates/RHIZOME.md) describe explicit Rhizome operations well, but do not yet embody the selected proactive behavior for ordinary substantive work. The large reference inventory is organized substantially around tool families.

**Direction:** start from the decisions an agent needs to make: what knowledge might constrain this change, what evidence is relevant, whether it is authoritative/current, what durable knowledge changed, and what validation is warranted. The base experience must work without AE artifacts, starter names, or starter recipes. It should also compose with team-specific ontologies and workflows.

**Boundary:** proactive use does not mean mandatory bootstrap, ontology inspection, or new notes for every edit. A typo, a local implementation task, and a domain-heavy feature need different amounts of context. Existing general skill-authoring opt-in behavior is a distinct product contract; do not silently remove it while improving coding-task retrieval.

**Evidence needed:** ordinary bug fix, trivial edit, and degraded-capability cases in disposable no-starter repositories. Show useful retrieval and durable maintenance, not merely a routing answer that names Rhizome.

### 2. Retrieved material has different authority

**Observation:** the core instruction to treat all retrieved docs as operating constraints is too broad for a graph containing historical decisions, source excerpts, hypotheses, candidates, and accepted contracts. Structural relationships establish connections; they do not establish truth or authority.

**Direction:** distinguish current governing contracts from supporting evidence, historical record, and unresolved claims. Preserve provenance, lifecycle, uncertainty, and conflict when they exist. When metadata cannot establish authority, say so and inspect the underlying source; do not infer acceptance from retrieval rank or graph proximity.

**Boundary:** schema validation proves shape and structural consistency. It does not prove that a requirement is true, evidence is current, a contradiction is resolved, or every affected note was maintained. Do not invent an authority-ranking engine or broad source-change infrastructure in this staging scope.

**Evidence needed:** fixtures with a current decision beside a superseded one, a candidate requirement, conflicting source statements, and partial results. An agent should preserve uncertainty rather than promote it into accepted scope.

### 3. Preserve Agentic Engineering's process and make execution better

**Decision:** AE is the primary starter. Preserve spec → bounded effort → approved plan, plus the lightweight local-task route. Improve authorization continuity, context gathering during work, meaningful verification, durable updates, and fresh-agent handoff.

**Direction:** an approved plan authorizes its routine steps. An agent should not repeatedly ask permission for implementation details, authorized gates, or routine fixes. It should surface material scope or contract changes with a recommendation and evidence. Phase guidance should explain the decision and deliverable without prescribing an unnecessarily long itinerary.

**Boundary:** folding foundation review into the router, adding review phases, consolidating ingestion, and collapsing skills are candidates from the old draft, not user-approved requirements. There is no target skill count. A smaller skill can be worse if it removes an operationally necessary distinction. Preserve team-policy precedence and historical effort records.

**Sources:** [AE canonical sources](../../../pkg/app/cli/init/templates/starters/agentic-engineering/agents/skills/agentic-engineering/SKILL.md), [development loop](../../specs/process/development-loop.md), [review policy](../../engineering/review-and-approval.md), and [prior routing analysis](agentic-engineering-routing-evals-2026-09-04.md).

**Evidence needed:** an approved multi-phase feature and a fresh-agent resume must retain rationale, remaining work, and real verification evidence. A local task should stay local.

### 4. Complex Domain extends delivery; it does not replace the spec

**Direction:** sources, domain concepts, processes, workflows, and requirements explain what constrains the delivery contract. Keep requirements atomic, candidates distinct from accepted obligations, processes distinct from actor workflows, and structural coverage distinct from semantic similarity. Maintain knowledge when delivery or source evidence changes without silently expanding accepted scope.

**Observation:** current skills and overlays repeat universal mechanics and can require broad retrieval before the actual decision is clear. Recipe and view outputs need meaningful handling of empty, partial, capped, stale, or degraded results. Existing deterministic recipes and typed relationships are strong foundations worth improving.

**Boundary:** do not assume that all source changes can be found automatically; broad source-change infrastructure is deferred. Initial source-change scenarios can supply a known changed source. The SPEC-0062 domain-context query returned no linked requirements or feature/process/workflow notes in the assessed repo; that is a coverage gap, not absence of constraints.

**Dependency:** the coordinator reconciles and closes the earlier active Complex Domain delivery against its original scope before redesign implementation changes its spec or starter assets. New planning can proceed now. AE phase and overlay contracts must be agreed before either side implements changes to them.

**Sources:** [Complex Domain spec](../../specs/product/complex-domain-starter.md), [canonical router](../../../pkg/app/cli/init/templates/starters/complex-domain/agents/skills/complex-domain/SKILL.md), [recipes](../../../pkg/app/cli/init/templates/starters/complex-domain/rhizome/query-recipes/complex-domain.yaml), and [overlay contract](../../specs/technical/skill-template-overlays.md).

### 5. Rhizome already has valuable safe-write machinery

**Observation:** [projection](../../../pkg/ontology/projection.go), [edit sessions](../../../pkg/ontology/edit_session.go), and [validation transactions](../../../pkg/validate/transaction.go) already provide substantial structure for preserving source content, staging changes, previewing differences, handling conflicts, and applying repairs. Current agent guidance generally routes structured authoring through direct Markdown edits followed by validation, while richer edit sessions are primarily exposed through the web experience. This is a guidance and source observation; behavioral evaluation has not run.

**Direction:** consider exposing selected existing typed operations to agents instead of creating another mutation engine. Keep preview, diff, apply, base/current identity, warnings, authorization, and conflict evidence visible. Raw narrative authoring and safe structured edits are different needs; neither must replace all of the other.

**Current-main constraint:** preserve [preview lineage and replay conflicts](../../specs/technical/ontology-edit-replay-conflict-contract.md), `canonicalRef`, and usable durable locators. A planned block ID cannot be cited as frozen scope until it resolves; retain `requiresFix` information. The old code-mode branch predates these changes.

**Evidence needed:** edit succeeds against the intended identity, concurrent source changes produce a meaningful conflict, unsupported/ambiguous targets stay explicit, and repeated apply/recovery follows the existing contract. Exact write coverage is selected in the code-mode plan, not assumed to include every operation.

### 6. Code mode needs progressive discovery and an honest comparison

**Decision:** include a bounded code-mode effort, using PR #157 as source material. Do not import that branch wholesale. At assessment it conflicted with main and was 92 main commits behind the staging baseline.

**Observation:** the branch's `pkg/app/agentcode/surface.go` eagerly returns every operation plus the full schema bundle. Its latest fixture was roughly 164 KB, about 41K estimated tokens. The PR's earlier four-call comparison favored direct calls by roughly fourfold in visible tokens. Those are branch/historical measurements, not fresh measurements of current main or a general verdict on code mode.

**Direction:** compact initial discovery, selected operation descriptions, and the required transitive schema definitions. Keeping a full generated SDK on disk is different from loading it into model context. Use code for predictable orchestration, joins, deduplication, filtering, and aggregation; return source evidence, warnings, and incomplete-result indicators for model judgment. Do graph traversal server-side when GraphQL already expresses it well.

**Critical baseline:** coding agents already script CLI calls and submit batched GraphQL. Compare against competent use of those tools, not an artificially serial sequence. Typed discoverability, consistent outputs, shared setup/runtime, and safe semantic operations must earn the extra surface. Compare both first use and repeated use; simple lookups must stay cheap.

**Current-main constraints:** [the existing catalog](../../../pkg/app/agentapi/catalog.go) and [agent-surface invariants](../subsystems/agent-surface.md) already own dispatch identity and CLI/MCP parity. Minimal startup must not reacquire global indexing, stores, providers, or full ontology loading. Design around the current single catalog; do not introduce a second registry merely to port the older design.

**Open design:** native host programmatic calling, a thin generated client, and a bespoke runtime have different costs. Select the minimum that meets demonstrated workflows. Public operation contracts, transport/version behavior, stale artifacts, and semantic writes need explicit review before dependent implementation.

**Primary research:** [OpenAI programmatic tool calling](https://developers.openai.com/api/docs/guides/tools-programmatic-tool-calling), [OpenAI tool search](https://developers.openai.com/api/docs/guides/tools-tool-search), and [Anthropic advanced tool use](https://www.anthropic.com/engineering/advanced-tool-use) support composing calls and loading relevant definitions. They do not prove a Rhizome-specific improvement or guarantee success across models.

### 7. Readiness and retrieval claims need current evidence

**Assessment leads:** capability advertisement, actual handler readiness, index freshness, and operation success are not interchangeable. Initial probes also identified exact-code confidence and broad test-scanning costs worth rechecking. These are candidate targeted improvements, not an approved new performance program.

**Boundary:** the initial checkout had index/runtime issues; the current staging CLI was subsequently built and documentation validation passed. Do not copy the earlier failure into a claim that today's staging integration is broken. Conversely, successful documentation validation is not proof that every search or graph capability is ready.

**Direction:** expose operation-specific truth, explicit gaps, and useful remediation. Prefer bounded code↔documentation bindings and exact evidence over broad repeated scans when known targets are available. Add an operation or warning contract only when a concrete scenario demonstrates the need.

## Preserve context during execution

1. In the first plan, state the relevant accepted constraints, the most important unresolved tensions, and which assessment claims were reverified or corrected. This is part of the planning artifact, not a new approval ceremony.
2. Give each worker a bounded question or change, exact owned files, relevant contract excerpts and source links, known failure examples, and required observable evidence. Pass the reasoning behind a constraint when violating it would otherwise look like a simplification.
3. Keep decisions with the owning effort or durable contract as they happen. Record evidence, rejected alternatives that explain a tradeoff, deviations, and what would invalidate the conclusion. Do not leave material decisions only in chat or PR comments.
4. Notify the coordinator when a shared contract changes. It owns propagation to other efforts and the combined staging record. Workers propose shared spec edits; the coordinator integrates them to avoid competing definitions.
5. A child PR reports its actual head, affected paths, verification, limitations, and outstanding decisions. The supervisor inspects the result. The coordinator independently reviews integration-sensitive changes against the combined staging head.

At handoff or compaction, retain the effort path, current branch/commit/PR, accepted scope, open decisions, completed evidence, next action, and relevant source links. Read the latest durable record instead of relying on a copied conversation summary. Append evidence as work proceeds; do not repeatedly duplicate this entire note into every artifact.

## Questions still belong to the plans

Exact skill topology, phase/overlay changes, the initial operation set, code-mode transport, and the evaluation protocol are not settled by the high-level direction. Each supervisor should make a source-grounded recommendation, identify the smallest consequential decision, and continue independent planning. The coordinator presents one coherent implementation plan for approval rather than five disconnected design discussions.
