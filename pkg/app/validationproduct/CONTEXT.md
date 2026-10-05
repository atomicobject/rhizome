## Validation product projection orchestration

Composes suite selection with validation projection evidence and prepared-runtime execution. `RunWithProjection` stays read-only; production command wiring resolves one canonical vault/config/runtime and hands its reviewed result to `pkg/validate.ApplyRepairSession` for explicit apply.

- **Entry point**: `RunWithProjection`.
- **Refresh admission**: `RefreshCoordinator` records the replacement generation before canceling the active run. A replacement that fails admission leaves the active run in control; an admitted replacement makes older completion return `ErrRefreshSuperseded`.
- **Recipe applicability**: live and command paths resolve authoritative selection before feature facts and call `queryrecipe.HasDefaultSources` only when query-recipes is selected. Discovery covers all default roots, including broken root symlinks as diagnostics. Recipes and load diagnostics make the check applicable; empty roots and unrelated skills do not. The check reloads sources to publish their findings; feature discovery retains no cached inventory.
- **Key invariants**: preflight pending repair journals, then repeat the check through `BeforeMutation` under the canonical index lock before any projection store opens; CI uses scratch and cleans its temporary DB; local/agent use live; one refresh/runtime/close lifetime; never fall back to `EnsureFreshRuntime`. Caller-supplied `RunContext` must match the exact projection root. Scratch CI cannot consume a live code index, so blocked guidance says run locally or remove the check from CI composition.

### Deep docs

- EFF-2026-07-15-19-43-4
- EFF-2026-07-15-19-43-6
- [[validate]]
- [[indexing]]
