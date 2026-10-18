## Neutral validation selection, applicability, result, and exit contracts

Owns configuration, feature facts, selection, applicability, and result composition for CLI, live refresh, and indexing. Indexing can consume this package without depending on the projection adapter, avoiding an import cycle.

- **Entry points**: `Prepare`, `RunValidation`, `RunPrepared`, `IndexValidationSnapshot`, `BuildValidationResult`, `AttachFixExecution`.
- **Key invariants**: one selector; every effective check has one typed outcome; completed checks alone execute; exit 2 dominates findings. Preserve the caller's exact `ApplyCommand` while rebuilding `NextActions`, so applicability expansion cannot lose selector/vault/scope authority. Preserve additive suite evidence such as `identifierReconciliation`; when identifier execution is attached, fold its non-JSON apply/postcheck timings into that public six-stage envelope.

### Deep docs

- EFF-2026-07-15-19-43-4
- [[validate]]

`RunPrepared` probes metadata and ontology readiness separately from the caller-owned store, reads code prerequisites from persisted facts, and never acquires a projection or index lock. `ValidationCheckOutcome` embeds the applicability contract while adding remediation guidance.

Read results carry action summaries with `PreparationRequired`, without executable operations or whole-file replacement bytes. Explicit repair preparation belongs to `validate.PrepareRepair` at command/review boundaries. Post-repair reduction preserves read-summary status while filtering guidance to the selected checks.
