---
summary: "Hub for bounded execution records in the Agentic Engineering workflow."
---

# Efforts

Efforts capture execution against frozen scope. [Review and approval policy](../engineering/review-and-approval.md) determines when work requires an effort. The [effort representation contract](../specs/technical/effort-representation-contract.md) defines the two supported forms.

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
