# Alignment

Compare delivered behavior, tests, docs, and effort claims against frozen scope, judging test evidence by `docs/engineering/testing-policy.md` and documentation obligations by `docs/engineering/documentation.md`. Load `query-recipe run --id closure-drift-pack` and `query-recipe run --id frozen-spec-index-pack` when equivalent current results are not already available; refresh sections changed since retrieval. Add `query-recipe run --id runtime-code-evidence-pack` for changed code when it could resolve a contract question, and use the detail pack only for a specific unresolved contract. Run `validate frozen-scope-drift`; its findings are a closure gate.

Produce findings first: missing criteria, unsupported claims, stale links, unrecorded deviations, and evidence gaps. Classify each as safe fix, `needs decision`, `historical/frozen`, `unrelated/pre-existing`, or `out of scope` so reconciliation can act without re-deriving judgment.

Mechanical corrections and in-scope review fixes proceed within the approved authority. Stop for unresolved truth, scope, or acceptance decisions; record the mismatch in the effort instead of smoothing it over.
