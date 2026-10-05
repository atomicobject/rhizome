## Neutral validation selection, applicability, result, and exit contracts

Owns product orchestration below CLI renderers and projection adapters so neither depends upward on the other.

- **Entry points**: `RunValidation`, `BuildValidationResult`, `AttachFixExecution`.
- **Key invariants**: one selector; every effective check has one typed outcome; completed checks alone execute; exit 2 dominates findings. Preserve the caller's exact `ApplyCommand` while rebuilding `NextActions`, so applicability expansion cannot lose selector/vault/scope authority. Preserve additive suite evidence such as `identifierReconciliation`; when identifier execution is attached, fold its non-JSON apply/postcheck timings into that public six-stage envelope.

### Deep docs

- EFF-2026-07-15-19-43-4
- [[validate]]
