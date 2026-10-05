# Planning

Use `references/batch-plan-template.md` to structure the work around verifiable outcomes. Deliverable: an executable plan written into the effort: current gap, owning modules and contracts, delivery batches, tests, documentation, validation, and explicit batch exits that define completion. Load `query-recipe run --id effort-execution-context`, then `query-recipe run --id frozen-spec-index-pack` for each frozen spec; use `query-recipe run --id frozen-spec-detail-pack` only when the index leaves a contract ambiguous.

Consult `docs/engineering/architecture.md` when placing code or crossing a boundary, `docs/engineering/testing-policy.md` when choosing evidence layers, `docs/engineering/documentation.md` for the documents the change must carry, and `docs/engineering/release.md` when the plan touches branching or shipping.

<!-- rzm:skill-slot id="integration-map.additional-dimensions" mode="extension" -->
<!-- /rzm:skill-slot -->

Surface each unresolved formative schema, public contract, ownership boundary, or reusable pattern as a named decision with its tension and recommendation. Treat settled decisions as constraints, not questions to reopen. When later phases depend on an unresolved decision, place it in an early decision batch and request a `foundation-review` exit before dependent work; otherwise no pause is implied.

<!-- rzm:skill-slot id="architecture-decisions.additional-checks" mode="extension" -->
<!-- /rzm:skill-slot -->

Decision boundaries: the human owns unresolved product, architecture, and approval decisions per `docs/engineering/review-and-approval.md`. If the effort already carries an approved plan, record routine choices in execution notes; revise the plan only for a material change, with the user's confirmation and a recorded deviation. Stop before edits only while such a decision is open; route scope changes to specification or effort setup with the exact decision.
