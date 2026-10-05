---
summary: "Hub for bounded execution records in the Agentic Engineering workflow."
---

# Efforts

Efforts capture execution against frozen scope. [Review and approval policy](../engineering/review-and-approval.md) determines when work requires an effort. The installed ontology and authoring guidance define the two supported forms.

## Choose a form

- Use Markdown `EffortNote` for simple work, keeping scope, plan, and lifecycle evidence inline. Existing Markdown efforts remain valid without conversion.
- Use an HTML `EffortWorkspace` folder when separate plan, work log, or supporting materials help. The entry owns identity and frozen scope; its canonical metadata explicitly selects `implementation-plan`, `work-log`, `governing-specs`, and optional `materials`.
- Use exact vault-relative component paths with extensions. The work log must be Markdown. Browser navigation links may be document-relative. `EffortMaterial` classification does not establish membership; only fields marked `@workspaceMember` select components for workspace navigation. Unlinked neighbors, backlinks, and folder proximity do not select components.

## Create and freeze

- Follow the installed Agentic Engineering effort-setup guidance for live ontology shape, allocation, timestamps, and context-specific escaping. Allocate `EffortNote` or `EffortWorkspace` from the shared `EFF` pool, never the `Effort` interface. Preserve the returned identifier and aliases after creation.
- Freeze exact selected specs and, where present, ready stories and acceptance criteria. Story increments may help selection but do not replace explicit links to embedded block targets. Requirements-only specs need no invented stories.
- Markdown keeps governing links in Spec Set (Frozen) and lists the same specs in `governing-specs` frontmatter; story links stay in Stories In Scope (Frozen). Workspaces keep frozen governing specs in entry metadata and exact story links on the overview; the log records the same selections and revisions. Do not add live effort links to selected stories.
- Keep approval absent until a real human grants it for an exact plan revision and scope. For HTML, author `plan-approved-by` metadata through a supported HTML surface or an authorized source edit. No CLI or agent root-metadata writer exists. A populated link, status, or work-log section is not approval.

## Record lifecycle evidence

- Markdown retains Scope, Spec Set (Frozen), Stories In Scope (Frozen), Spec Coverage Checklist, Plan, Original Intended Delivery, Actual Delivered, Execution Notes, Deviations, Compounding Follow-ups, Closure Checklist, and Status.
- The linked Markdown work log uses H2 sections: Spec Set (Frozen), Stories In Scope (Frozen), Plan Approval, Original Intended Delivery, Actual Delivered, Deviations, Closure Checklist, Status, and Execution Notes. These support lifecycle queries; HTML headings do not become ontology sections.
- Append execution events as work happens. Preserve frozen snapshots and record deviations and required approvals for material changes. Keep entry status and approval consistent with the log; surface disagreements before acting. The spec is not a running log.

## Resume and close

- Inspect `ref.typeName` on shared `Effort` results. Use `effort-execution-context` for planning or resume and `closure-drift-pack` for closure, anchored to the owning effort. Read complete plans, logs, and selected materials before decisions.
- Reconcile approval, frozen scope, actual delivery, deviations, and closure evidence with the current checkout. Existing approval continues across sessions while scope and authority agree; material plan changes require any necessary approval for changed work.
- At handoff, record current revision, verified outcomes, next unfinished item, blockers, and the next useful action. Close only under repository gate and approval policy. Completion does not authorize publication; complete and archived efforts remain historical.
- When release tooling collects effort evidence, `docs/engineering/release.md` names it and what it reads.

## Reusable document assets

Copy assets from `templates/`, replace every placeholder, and keep the inert `.template` sources unchanged:

- Markdown: `templates/effort.md.template` becomes a timestamped `.md` effort.
- Workspace: copy `templates/workspace/overview.html.template` to a timestamped HTML entry in the effort folder, alongside `plan.html`, `work-log.md`, and `effort.css` from their corresponding templates. Add a material from `materials/material.md.template` only when actual supporting content exists.
- Select exact metadata paths; remove unused examples and use empty arrays for absent governing specs or materials. These are authoring assets, not a runtime renderer. Follow effort-setup guidance for serialization and escaping before indexing.

Markdown frontmatter placeholders ending in `_yaml` take complete YAML string scalars, including their quotes. JSON-encode each string (for example, Go `encoding/json.Marshal`) to safely preserve quotes, backslashes, and newlines; JSON strings are valid YAML scalars. Use the same encoded identifier for `id` and `aliases`. `governing_specs_yaml` takes a JSON-encoded array of the wikilinks frozen under Spec Set (Frozen), for example `["[[docs/specs/product/checkout|SPEC-0007]]"]`; JSON arrays are valid YAML sequences. The body placeholder `effort_title` is separate: use Markdown heading text on one line. Replace placeholders in a single pass so inserted text is never interpreted as another placeholder.

HTML placeholders ending in `_json` take complete JSON values, including string quotes or array brackets, serialized with HTML escaping enabled (for example, Go `encoding/json.Marshal`). Escape literal `<`, `>`, and `&` as JSON Unicode escapes so text such as `</script>` cannot end the metadata element. The `governing_specs_json` and `materials_json` values are arrays of exact vault-relative paths; use `[]` when absent. Placeholders ending in `_html` take HTML-escaped text (for example, Go `html.EscapeString`), separately from JSON values. The timestamp in the navigation URL is the allocated numeric filename timestamp. Replace placeholders in a single pass so placeholder-like user text remains literal.
