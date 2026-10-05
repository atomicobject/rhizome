# Configured-view test ownership

- Prove execution, layout, and filter composition through `Service.Execute`; use the real source adapter for indexed behavior.
- Exercise each filter conjunct independently. A preset with no manual filters does not prove their intersection.
- Author expected rows and board values independently of production helpers. SQL/residual parity also needs literal expected results.
- Shared sort partition semantics belong to `pkg/ontology/pushdown`; remove unused view wrappers when their callers move.
- Preserve tests for alias resolution, source caps, provider identity, edit capabilities, and staged reads where those contracts are observable.
