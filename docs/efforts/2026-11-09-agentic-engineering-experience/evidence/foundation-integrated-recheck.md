# SPEC-0137 implemented-foundation recheck

VERDICT CHANGES. F1 remains OPEN, partially fixed. One verified P2 finding, retained under the existing F1 ID. No new verified adapter finding. This is an implemented-foundation verdict only, not UI, installation, foundation exit, PR readiness, or final acceptance.

## Checkout and authority

Confirmed cwd `/Users/colthorp/.t3/worktrees/rhizome/feat-agentic-engineerin-3e24586a9dbb78aa` and HEAD `d6624840de79e5153283c322709bb5141a90e5df` before and after inspection. Reviewed the frozen working-tree delta as well as committed adapter integration. Repository status remained the same. No repository edits, broad gates, builds, commits, external actions, credential reads, or subdelegation. The only file written by this reviewer is this report.

Reviewer ran in-thread on `codex/gpt-6.1-sol`, high reasoning effort. The explicit leaf and read-only brief superseded skill delegation and implementation steps. Applied foundation-review with agentic-engineering/rhizome context and consulted applicable policies and owning query/custom-view/CLI/ontology/noderead guidance. Entered through poteto-mode as required; the specific review brief controls the deliverable and scope.

Compounding lookup ran before unfamiliar code inspection. It reported `Atomic Object server unavailable: unlock the system keychain or run rzm auth login`. Retrieval is degraded. Its subsequent no-match message does not prove absence of relevant context shards. Evidence here comes from authored SPEC-0137, foundation-contract.md, the exact foundation-code-review.md ledger, source, actual platform callers/schema, and focused executable probes.

## F1. P2. Indented code still creates a false checklist observation after a thematic break

Status OPEN, partially fixed. The original standalone four-space or tab-indented example is now correctly excluded, and the real nested-task cases pass. The new containing-list tracking still mistakes a spaced thematic break for a list item.

Location `pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/lib/checklist.ts:25-29`.

Concrete Closure Checklist body:

```text
- - -

    - [x] Example command output
```

The installed `unified` + `remark-parse` + `remark-gfm` parser produces precisely two root children, `thematicBreak` and `code`, and no Markdown task items. The shipped `recordedChecklist` returns `state=recorded, checked=1, total=1`. The same happens with `* * *` as the separator.

The item regex treats the separator as a list item and pushes a content indentation of 2. That permits the following four-space-indented code through the relative-code guard. Passing that checklist into an active effort through the actual exported `groupResult('review', portfolio)` produces a `closure-checklist` observation. Both parser classification and the consumer observation were reproduced with assertions in Node. This is the same factual failure as the existing F1, not a separate finding.

Smallest correction: recognize thematic breaks before establishing list-item content indentation, preserving containing-list context where appropriate. Add a focused regression for the separator followed by the indented checkbox example, alongside the existing genuine nested-task cases. No parser dependency or broad redesign is needed for this demonstrated correction.

## Integrated seams inspected without additional verified findings

- Adapter hook signatures use actual TanStack `useQuery` options. Kit freshness classifies custom query keys as data; committed `graphql` and `readNodes` remain the transport owners. Abort checks prevent publishing a cancelled reader result; GraphQL does not accept an AbortSignal, so its in-flight request itself is not cancelled by this adapter.
- Query selection fields were traced against the public NodeRef, locator, workspace field/collection, NoteNode, Section, and source-link SDL. The public `structural` field and `structuralFingerprint: structural` alias preserve API identity and host navigation compatibility. `NodeLink` passes refs through to the existing navigation owner; host parsing handles GraphQL null identity fields. This was source inspection, not a live mounted UI test.
- `readNodes` supplies batches of at most 200, canonical deduplication/order, unresolved/errors/truncation, and aggregate generation. The adapter reads the own subject first, keeps the returned raw ref object, and retains own source/status when supplemental log transport fails. Own-record coverage and aggregate RecordDetail coverage remain separate.
- Summary inventory avoids whole-vault content. Detailed spec reads hydrate typed story and criterion collections in authored order and request full content. Effort components include explicit field-selected plan/log/material refs for subsequent presentation and prompt use. Final resume-prompt composition belongs to excluded UI integration and was not accepted here.
- Focused source traversal retains duplicate/unresolved occurrences, checks count/page continuity, revision and generation, and leaves short/failed/changed reads partial. Frozen-source, selector, target-hydration and inventory generation checks compose before claiming known selected scope. Exact criterion selection remains exact without promoting its containing story.
- Unknown frozen selection does not erase a known effort status or independently coherent checklist. Workspace checklist reads use the explicit work log and exact closure section. Missing, duplicate, unresolved, generation-changing and transport-failure cases are covered by the executed adapter suite.
- Review evidence is opt-in. Group coverage requires complete-effort scope and readable active checklists, without requiring planned/archive deferred scope. Home summary exposes review evidence as deferred and cannot itself certify absence of review observations.
- Source recipe changes expose sourceLinkCount, explicit offset zero and continuation/generation guidance. The effort-setup condensation retains its authority/mention distinction; the TypeScript config addition includes the matching Playwright config. Existing source-link count/page extension and optional frozenScope design remain settled decisions.

## Tests actually run

1. `node --experimental-strip-types --test pkg/app/cli/init/testdata/agentic-engineering/delivery.test.mjs pkg/app/cli/init/testdata/agentic-engineering/delivery-adapter.test.mjs` passed all 23 tests, with zero failures, skips or retries.
2. Inline Node comparison using installed unified/remark-parse/remark-gfm and shipped recordedChecklist confirmed the original standalone indented-code fix and the legitimate eight-space grandchild task. The thematic-break counterexample disagreed with GFM as described above.
3. Inline assertion-based Node reproduction verified both `- - -` and `* * *` produce GFM thematicBreak+code, while shipped recordedChecklist reports one checked task and actual groupResult emits closure-checklist. These assertions confirm the defect, not a successful behavior gate.

No broad checks, build, Go package tests, live API execution, installed lifecycle tests, browser tests, or application installation were run. Root's concurrent make check, its earlier line-cap failure, and the reported correction are external facts, not independently passed gates in this recheck. This reviewer did not read its log or certify its result.

## Acceptance boundary

Fix the remaining F1 counterexample and recheck that small delta before implemented-foundation approval. No other verified finding is raised. Final UI, browser/accessibility behavior, installed/update/eject lifecycle, final prompt composition, repository gates and PR readiness remain outside this review.
