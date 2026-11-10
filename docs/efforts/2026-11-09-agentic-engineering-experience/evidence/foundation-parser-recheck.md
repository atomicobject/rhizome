# Focused implemented-foundation parser recheck

The goal is to settle open F1 and the named foundation delta before UI integration continues.

VERDICT APPROVE for the implemented foundation within this recheck's scope. F1 is CLOSED. No verified new finding. This is not a foundation-exit, UI, lifecycle, installed-application, full-review, CI, or PR-readiness verdict.

## Snapshot and limits

Reviewed HEAD `c10f8e944efd4c1d7b30a31ff2bb1238ff70a6f3` plus the frozen working-tree delta in `/Users/colthorp/.t3/worktrees/rhizome/feat-agentic-engineerin-3e24586a9dbb78aa`. HEAD and the status listing were unchanged at the final inspection. Compared against `/private/tmp/ae-foundation-integrated-recheck.md`. Settled findings and the previously reviewed source-recipe/paging, referenceLines200, and Playwright configuration changes were not reopened.

Read AGENTS.md, review and approval policy, foundation-review, agentic-engineering/rhizome guidance, custom-view guidance, and the foundation contract. Entered through poteto-mode. The explicit leaf, read-only brief overrides delegation, implementation, and general explanation deliverables in skills. Review ran in-thread on codex/gpt-6.1-sol with high reasoning effort. Throughput checkpoint is not applicable to this read-only review. Type System Discipline informed checking the adapter's factual checklist variants and the field-link type contract against its producer.

Compounding lookup ran before unfamiliar source inspection. It reported `Atomic Object server unavailable: unlock the system keychain or run rzm auth login`. The Rhizome-integrated retrieval experience is incomplete at that layer. Its subsequent no-match message does not prove no relevant shard exists. This verdict uses supplied evidence and inspected source, not successful semantic retrieval.

## F1 closure

The prior recommendation to add another regex exception is superseded by the disproven approximation premise. The private `delivery/lib/checklist.ts` implementation and barrel export are removed. `web/kit/markdown-checklist.ts:6-28` uses installed unified, remark-parse, and remark-gfm and counts only listItem AST nodes with boolean checked values. Descendant traversal includes nested and quoted task items and does not turn code text into list items.

The actual Markdown component uses react-markdown with remark-gfm (`web/src/components/markdown/Markdown.tsx`); its installed react-markdown package depends on remark-parse and unified. Installed package metadata confirms remark-parse 11.0.0 and unified 11.0.5. The manifest and lockfile diff promote these two existing packages to direct dependencies. The lockfile changes only the root dependency declarations, with no node_modules package-entry/version changes.

`web/kit/markdown-checklist.test.tsx:5-22` contains ten explicit expected-count cases and renders the real kit Markdown component for each case, asserting both total and checked checkbox counts. It includes both F1 thematic-break-plus-indented-code examples, standalone indented code, fenced code, quoted tasks, genuine nested/deeper tasks, a nested code example, prose, and an empty marker. This verifies the grammar boundary rather than preserving another regex approximation.

Direct callers were inspected. The kit exports the helper; `delivery/data.tsx:7` supplies it through ReadAPI; `delivery/reads.ts:182-193` invokes it only for a coherently readable closure section and normalizes missing, unreadable, empty, and recorded states. Deferred summary checklists now remain unreadable rather than falsely missing. `groups.ts` consumes recorded positive counts only as an authored-checklist observation. Fully checked tasks do not change effort status or establish acceptance.

## Named delta

- `reads.ts:172` checks frozen-section diagnostics before accepting a single section target. Its direct `material` and `frozenScope` consumers retain an incomplete state when diagnostics exist. The committed adapter regression injects a cardinality issue and asserts incomplete scope.
- `groups.ts:99-115` requires complete frozen scope for complete efforts and readable checklist states for active efforts. Planned and archived deferred scope does not prevent a complete review result. The focused domain case covers those distinctions.
- `reads.ts:269-285` deduplicates explicit selected refs, calls the existing record reader without inventory traversal, preserves unknown/incomplete scope, and combines own and selected-scope generation evidence. The direct hook in `data.tsx:39-47` uses the existing query lifecycle, activity/subscription controls, and cancellation signal. The adapter case confirms duplicates do not duplicate results and other effort inventory rows are not read. UI selection and Copy wiring remain pending and excluded from this approval.
- `reads.ts:231-238` retains explicit implementationPlan/workLog values and obtains canonical locators only from matching hydrated field targets. The actual `resolveWorkspaceFieldLinks` producer in `pkg/ontology/query/workspace_field_links.go:217` emits one link per authored value in the same order and returns null refs for unresolved values, supporting the adapter's indexed correspondence. `prompts.ts:38-44` emits verified locators or labels the exact authored reference as unverified. It does not derive canonical targets from proximity or raw text. Pure prompt tests assert those two forms.
- The foundation contract and kit reference now document the parser boundary and selected-context read contract. The contract's future summary-selection behavior is not evidence that UI integration has already happened.

## Evidence and gaps

Inspected `/private/tmp/ae-parser-adapter-focused.log`. It records 23 passing pure and adapter tests with zero failures, skips, or retries. Inspected their source, including the fake API helper that asserts the exact closure-section text before returning counts. That fake tests the adapter boundary; it is not evidence for parser grammar. Grammar evidence comes from the ten real renderer/count test cases and root's reported successful `npx vitest run kit/markdown-checklist.test.tsx` execution. This reviewer did not independently rerun that command or observe a separate saved Vitest result.

No tests, builds, gates, executable probes, installs, commits, pushes, PR actions, watches, credential reads, or delegates were run by this reviewer. No additional executable probe is necessary for this bounded conclusion. The concurrent full gate is not certified here. Only this temporary report was written.

Root still owns UI integration, actual installed proof, full implementation review, successful repository gates, current-head CI and required review evidence, and the requested open unmerged PR. No UI or lifecycle readiness is implied.
