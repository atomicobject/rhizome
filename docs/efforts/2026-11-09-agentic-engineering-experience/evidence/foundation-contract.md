# Delivery foundation contract

This is the integration owner's selection from the two architecture sketches. It refines the implementation plan draft before code starts. Independent technical review is pending. It does not record human plan approval.

## Authority and design selection

Drew's binding directive was, "Start a new chat with GPT 6.1 extra high using a Team of 6.1 high subagents to spec and implement this entire thing full stop." The implementation brief additionally authorizes the necessary minimal shared fixes, requires a reviewed plan and real installed UI proof, and leaves the PR open and unmerged. This authorizes completing the proposed scope and resolving its routine implementation details. It is not a claim that Drew reviewed a plan written afterward. The configured current user is absent, so `plan-approved-by` stays absent.

Keep the draft's five primary work views in the Shape, Build, Settle map. Shaping specs and Ready stories overlap by design. Planned and Active use recorded effort status. Delivery history retains complete and archived as distinct states. Current contracts and Record review are separate supporting views. All active efforts remain Active even when every closure checkbox is checked. This keeps the comp's common visual map while avoiding a second synthetic delivery lifecycle. The independent seven-view alternative's exact-scope review signals and source-generation checks are retained.

Ready stories may belong to proposed or active specs. The list and prompt identify a proposed parent explicitly and route it through specification before freezing delivery scope. Existing effort mentions do not remove ready stories. Every count names its unit, and overlapping counts are never added together.

No new approval queue, acceptance-ready label, automatic lifecycle write, policy browser, generic site framework, or custom agent invocation is introduced.

## Explicit frozen scope for HTML workspaces

Plan review P1 identified that HTML entry headings are not ontology sections. Pagination cannot identify which entry links are frozen scope. Resolve that gap with one optional `frozenScope: EffortMaterial` relation on EffortWorkspace, authored as `frozen-scope` metadata and restricted to explicit field values. It uses the existing material type and its typed Spec Set (Frozen) and Stories In Scope (Frozen) sections. The entry owns the choice of authoritative source. Directory proximity, the work-log field, and body links never supply this relation.

New workspace templates explicitly select their existing Markdown work log as `frozen-scope` using the already available work-log path placeholder. No additional file or template input is required. Teams can select a separate scope material when their work log should remain a secondary snapshot. The selected material's frozen sections are authoritative; HTML Frozen scope prose is an entry summary. The work log is a separate snapshot only when it is a different source. When both fields select the same file, show that fact once and do not invent a discrepancy.

Existing workspace records are not rewritten or silently migrated. Without an explicit frozen-scope field, the brief shows the original HTML entry and labels any log selections as recorded log scope, with authoritative frozen membership unavailable. It never reports no selected stories. Broken or unsupported selected materials remain visible with their authored reference. A selected source with absent or duplicate frozen sections is incomplete, not empty. Governing-spec metadata remains separate from selected frozen-spec occurrences.

Update the starter ontology, workspace template, effort authoring guidance, affected recipes, and their tests together. Current sources that describe the HTML entry as the frozen-scope owner must explain this explicit source selection. Preserve the entry's identity, status, approval, and authored content. No generic HTML parser or heading-node implementation is added.

The foundation fixture selects story A through the explicit frozen-scope material, mentions B in the HTML entry, and records C in a different work log. The public API and final brief must identify A as frozen, B as an entry mention, and C as a log snapshot. Include a second fixture without the new field to prove legacy uncertainty, and one selecting a criterion without its whole story.

## Shared source-link extension

Use the existing focused projection and resolver. Add only these public fields and arguments:

```graphql
sourceLinks(first: Int = 100, offset: Int = 0): [NodeWorkspaceSourceLink!]!
sourceLinkCount: Int!
```

`sourceLinkCount` counts authored occurrences in the focused projection before slicing, including duplicates, unresolved targets, unsupported targets, and embeds. It requires no target hydration. It is not a count of resolved or distinct targets.

Offset is zero based. A negative offset is a query error. Offset at or past the count returns an empty page. Preserve current `first` behavior, including the maximum of 100 and an empty page for nonpositive values. Apply offset and limit without integer overflow. Preserve source order and resolve only the selected page through the existing request scope. Count and page use the same projection. Existing callers retain their first page when they omit offset.

Use this count rather than adding a parallel page-info structure. A page is complete only when accumulated occurrence count reaches the reported count without errors. A short page before that count, missing generation, or changed source revision or graph generation produces a visible partial result. There is no indefinite retry loop.

Export the existing `responseGeneration` accessor from the kit entry point if its inspected contract supports this use. It exposes the generation attached to existing kit reads, without adding a new protocol. The starter's private traversal combines pages only when source revision and graph generation agree. It uses the kit's existing generation-aware requests and query lifecycle. Typed-root inventory pagination applies the same consistency rule. A target rename can change resolution without changing the source text.

Do not add an AE-specific GraphQL root, lifecycle field, generic pagination framework, client-side link parser, or inferred frozen relation. Focused source-link occurrences own provenance; `readNodes` owns bounded hydration. Their order and coverage remain separate.

## Starter interface and ownership

The experience lives under `pkg/app/cli/init/templates/starters/agentic-engineering/module/views/delivery/`. Its pure library owns factual group predicates, canonical identity, coverage composition, recorded checklist states, exact selected scope, and prompt construction. The kit's markdownChecklist helper counts task items with the same installed GFM parser used by Markdown rendering; the starter does not approximate Markdown with a private parser. React data hooks adapt the shared query results. Pages and the panel consume those facts and never duplicate the rules.

Home and list reads use summaries first. Record pages read their own subject before wider orientation. Bodies are scoped to selected records or the explicit candidates needed for a review observation. A generic inbound story effort relation is a mention, never frozen selection. Header governing links, frozen spec occurrences, and work-log snapshots remain distinct.

The source-link owner changes query implementation, tests, and owning documentation. The domain owner changes `delivery/lib/` and `delivery/data.tsx` plus tests. The UI owner changes home, list, spec, effort, panel, shared presentation, and CSS. The packaging owner changes registration YAML, installer lifecycle tests, disposable browser fixtures, and E2E wiring. Root owns the spec, effort, integration, changelog, final proof, and PR. Each writer gets a T3-managed worktree from the committed contract. Root integrates and checks their output before reporting it.

Keep the existing four native collection IDs and their behavior. Use concrete spec and effort node mounts, one hidden list view with declared route parameters, and the existing group and panel host. No generated installed skill or view copy is an implementation source.

The registration contract is `agentic-engineering.home` on group Delivery, `agentic-engineering.stage` as a hidden standalone view with identity parameter `stage`, and `agentic-engineering.panel`. Stage values are `shaping`, `ready`, `planned`, `active`, `history`, `contracts`, and `review`; validate unknown values with an explicit invalid-route state. Stage links use standalone context. Concrete record mounts are `agentic-engineering.spec.process`, `.product`, `.technical`, `.experience`, and `.operations`, and `agentic-engineering.effort.note` and `.workspace`. They default to `spec.tsx` or `effort.tsx` respectively. Spec definitions declare a nonidentity `story` parameter for opening and focusing an inline story. The panel registers through the experience host. The four existing native definitions stay in place with their IDs and behavior.

## Foundation exit

Focused tests prove occurrence counts and order at 0, 99, 100, 101, 200, and 201; negative and out-of-range offset; nonpositive and oversized first; repeated and unresolved links; same-file anchors; focused-section boundaries; aliases; and count-only reads without target hydration. Introspection, query validation, runtime behavior, and documentation agree.

Domain tests prove mixed story states, overlapping groups, criterion-only selections, mentions outside frozen scope, missing approver metadata, separate governing and frozen sets, and partial or changed-generation reads. Checked boxes do not establish acceptance. A completed effort with an unselected ready story does not create a maintenance finding.

Independent foundation review covers the implemented API and domain rules before dependent UI integration. Review fixes within this contract remain authorized. A failed check remains a failed check. The final installed UI, all major states in both themes, native alternatives, install/update/eject behavior, full repository gates, independent implementation review, Greptile 5/5, and current-head CI are still required.

Selected effort rows load only their selected execution contexts before composing a prompt. Summary rows remain selectable by their known canonical record facts, while Copy stays disabled until their selected scope is known. Prompt context retains explicit plan and log references, with unresolved authored values labeled as unverified.
