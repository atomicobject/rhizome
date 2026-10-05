# Effort Setup

Deliverable: one effort that freezes durable links to the exact specs and selected scope, with a plan, execution evidence, deviations, follow-ups, and closure evidence. A scope argument is a candidate slice, not approved frozen scope.

## Select scope

- Freeze durable links to the exact specs, requirements, stories, or criteria that apply; record exclusions and uncertainty. Requirements-only specs need no invented stories.
- Use `query-recipe run --id story-acceptance-pack` when selecting stories or criteria. Freeze a locator only when `requiresFix: false`. A planned block ID is a proposed source edit, not a durable link. When repair is authorized, apply it through a write-capable Rhizome surface and rerun the recipe. Otherwise freeze the nearest durable parent and record the precise target as follow-up.

## Choose and allocate

- Use Markdown `EffortNote` for simple work. Use an HTML `EffortWorkspace` folder when a separate plan, append-only work log, or selected materials help. Preserve historical Markdown efforts without conversion.
- Load `query-recipe run --id runtime-authoring-context --anchor <concrete-type>`. Allocate the concrete type, never the shared `Effort` interface; both forms use the shared `EFF` pool.
- For `DATETIME`, choose the prospective timestamped entry path first with local wall time (`date +"%Y-%m-%d-%H-%M"`), then call `next-id --type <concrete-type> --path <path>`. For `SEQUENTIAL`, use `next-id --type <concrete-type>`. Follow the live strategy; do not migrate it from this skill.
- Persist the returned id verbatim in `id` and `aliases`. Keep it stable after moves. Same-minute, repeated-DST-minute, and cross-zone collisions can require deterministic suffixes.

## Author the chosen form

- Markdown keeps its plan, frozen selections, and lifecycle evidence inline. Set `governing-specs` frontmatter to wikilinks for the same specs frozen under Spec Set (Frozen), so `governingSpecs` on the shared `Effort` interface reaches them. A shared `Effort` result does not guarantee an inline plan; inspect `ref.typeName`.
- The workspace entry owns identity and frozen scope. Explicitly select `implementation-plan`, `work-log`, `governing-specs`, and optional `materials` in canonical HTML metadata using exact vault-relative paths with extensions. The work log must be Markdown. Browser navigation links may be document-relative.
- `EffortMaterial` classifies plan, log, and supporting files; classification alone does not establish membership. Neither nearby files, body links, nor backlinks fill component fields. Folder inference remains a future opt-in interpretation, not current behavior.
- Keep exact story and criterion links on the owning overview. Mirror frozen selections and revisions in the linked log. Its H2 sections are Spec Set (Frozen), Stories In Scope (Frozen), Plan Approval, Original Intended Delivery, Actual Delivered, Deviations, Closure Checklist, Status, and Execution Notes. HTML headings are not ontology sections.
- Read complete linked sources before decisions. Surface disagreement between overview, plan, and log before acting; log snapshots do not override entry identity or scope.
- Use UTC second precision (`date -u +"%Y-%m-%dT%H:%M:%SZ"`) for actual creation, approval events, and history. Creation time is independent of the local filename stamp and must not be replaced by approval time.
- Generate HTML metadata with a JSON serializer that keeps `<`, `>`, and `&` escaped, so literal `</script>` becomes `\u003c/script>`. Escape HTML text and quoted attributes separately. Replace template placeholders before indexing. Do not apply Markdown note mutation operations to HTML; use an explicitly supported HTML surface or authorized source edits, then validate.

<!-- rzm:skill-slot id="scope.additional-context" mode="extension" -->
<!-- /rzm:skill-slot -->

## Approve and execute

- Approval authority: the human approves frozen scope and the plan. Leave `plan-approved-by` absent until a real human grants approval; never infer it from a plan link, status, template, or parsed section. Record the actual approver and exact approved plan revision and scope.
- For HTML, `plan-approved-by` is metadata authored through a supported HTML surface or an authorized source edit. No CLI or agent root-metadata writer exists. Recording evidence in the work log alone does not write entry metadata or grant approval.
- Existing approval continues across phases and sessions while scope and authority agree. Continue to planning when authorized; route uncertain behavior to specification. Material changes require a recorded deviation and any required new approval before executing changed work.
- Append decisions, surprises, learnings, validation, deviations, and follow-ups as they happen. Routine execution notes do not revise the approved plan. Do not silently widen frozen scope or backfill history at closure.
