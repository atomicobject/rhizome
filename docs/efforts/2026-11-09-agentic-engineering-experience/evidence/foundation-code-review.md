# SPEC-0137 implementation foundation review

Review the implemented foundations before adapter and UI integration, while preserving the approved frozenScope decision.

VERDICT CHANGES for this foundation scope. One verified P2 correctness finding. No verified P1 finding. This verdict does not assess foundation exit or final acceptance.

## Checkout and scope

- Working directory `/Users/colthorp/.t3/worktrees/rhizome/t3-83c409d2-ea5dcc444886b446`.
- Branch `t3/review-delivery-foundations`.
- HEAD `f76e49f7d68c0ea6b29bb9b842b2874653bc2d7e`.
- Reviewed changes against `019a59ea128b900b57dfbcf565481f10ea4e9553`.
- Repository working tree was clean before and after review. No repository edits, commits, delegation, PR actions, or application installation.
- Reviewer ran in-thread on `codex/gpt-6.1-sol`, high reasoning. Explicit leaf restriction superseded skill delegation steps.

Read AGENTS, applicable engineering policies, foundation-review and owning query/ontology/noderead/CLI/custom-view guidance, the final foundation contract, and current SPEC-0137. The approved optional selector design was treated as settled.

Compounding lookup ran before code inspection. It reported `retriever_degraded` because the Voyage endpoint could not resolve. Its no-match result does not establish absence of relevant context shards. Review evidence came from committed contracts, code, callers, fixtures, and focused tests.

## Findings ledger

### F1. P2. Indented code creates a false checked-checklist observation

Location `pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/lib/checklist.ts:19`.

The task regex accepts arbitrary leading whitespace without tracking indented Markdown code blocks. A Closure Checklist section whose entire body is the following four-space-indented example has no Markdown task item:

```text
    - [x] Example command output
```

The exported `recordedChecklist` nevertheless returns `{"state":"recorded","checked":1,"total":1}`. Passing this result into an active effort and calling `groupResult('review', portfolio)` produces a `closure-checklist` observation. I reproduced both results with the shipped exports in Node. The reachable consumer is `groups.ts:28`, which turns these counts directly into an observation. Existing coverage excludes fenced examples, but does not exercise plain indented code.

This violates the factual rule requiring a nonempty, fully checked recorded checklist and can tell a lead to inspect closure evidence solely because a code example looks like a checkbox. Source-preserving section reads retain indentation, so this is a valid input to the library, not a fabricated malformed adapter value.

Minimal correction: exclude indented Markdown code from task counting while retaining genuine nested list tasks. Add a small behavioral regression covering this input and the existing nested-task case. Do not replace this with a global indentation cutoff that discards legitimate nested tasks.

Status OPEN. No fix applied by this reviewer.

## Findings checked without additional defects

- Source-link count is taken from the same focused projection before slicing. Offset slicing avoids offset-plus-first overflow, preserves occurrence order and duplicates, and resolves only the requested page through the existing scope. Prepare validates negative offsets, including aliases and variables. SDL defaults, discovery/introspection, golden surface, overlay and hydration tests are coordinated.
- Kit exports point directly to the existing responseGeneration accessor and Markdown component. Existing generation storage and readNodes aggregation remain the owners of response facts. No second renderer or link parser was introduced. Bundling uses the existing kit build graph.
- frozenScope is optional, metadata-only, and explicitly marked workspaceMember. Templates select the existing work log through the existing placeholder. Guidance and both context/closure recipes distinguish authoritative frozen sections, governing metadata, entry mentions, and a separate log snapshot. Public recipe tests exercise missing, broken, unsupported, duplicate, same-source, and criterion-only sources.
- The story-acceptance caller correction is necessary: selection through a material need not produce an inbound Effort neighbor on the story. Its revised guidance does not treat an empty relation as unclaimed scope.
- Pure scope logic retains occurrence order and unresolved authored references, does not promote criterion-only selection to its story, and separates legacy unknown authority from empty scope. Coverage composition rejects mismatched revisions/generations and incomplete occurrence traversal. Five primary groups and two support views preserve recorded states and overlap. Checked active efforts remain active. Prompt text preserves existing authority and does not infer missing human approval.
- The synthetic A/B/C fixture has explicit selected A occurrences, entry mention B, separate log C, unresolved scope, and differing governing metadata. Its presence is useful foundation evidence, not a final UI pass.

## Tests actually run

1. `node --experimental-strip-types --test pkg/app/cli/init/testdata/agentic-engineering/delivery.test.mjs` passed all 12 tests.
2. `GOCACHE=/private/tmp/ae-foundation-go-cache go test -tags fts5 ./pkg/ontology/query -run '^TestWorkspaceSourceLink' -count=1` passed, package test time 0.508 seconds.
3. `GOCACHE=/private/tmp/ae-foundation-go-cache go test -tags fts5 ./pkg/app/cli/init -run '^TestAgenticEngineeringFrozenScopePublicRecipes$' -count=1` passed, package test time 0.761 seconds.
4. An inline Node reproduction imported recordedChecklist and groupResult from the shipped library. It confirmed F1 with checked=1, total=1 and observations=["closure-checklist"]. No temporary repository test was added.

No retries, skips, timeout changes, broad gates, or concurrent builds were used. The root's reported baseline make check and pending broad-gate evidence are external evidence, not checks rerun or independently certified here.

## Remaining review boundary

Final live adapter, UI, installed lifecycle, accessibility, browser behavior and integrated source-generation checks were excluded as requested. Web dependencies are absent in this isolated checkout, so kit unit tests and a kit bundle build were inspected but not executed. Current kit exports have no verified defect from that inspection. Adapter integration still needs to prove canonical public refs, response generation, source revisions, retained partial evidence and exact selection ordering together.

Fix and recheck F1 before approving these foundation pieces. An eventual APPROVE applies only to the implemented foundations and must not be represented as foundation exit, UI readiness, or final acceptance.
