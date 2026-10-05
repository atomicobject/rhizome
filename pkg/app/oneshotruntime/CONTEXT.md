## Explicit runtime plans for one-shot operations

Declares what a one-shot command may initialize, await, read, and mutate without changing `LiveRuntime` ownership.

- **Entry points**: `Plan.Validate`, `BuildAndAwait`, `DefaultRegistry`
- **Key invariants**: capability slices always compile to explicit bootstrap requirements; build and await are separate; current CodeIndex freshness is declared per plan rather than selected by operation name; readiness stays outside `ToolDescriptor`; bootstrap never imports this package

The registry inventories executable fronts. Request planners remain next to normalized command input until their rollout phase.

`OntologyQueryPlan` is the shared compiled-query planner. It always requests and
awaits CodeIndex/current ontology projection; it adds Semantic only after
`PreparedQuery.UsesSemantic` is known. One-shot composition disables watcher,
leader, session, and incidental provider work, while `EnsureFreshRuntimeWithStore`
remains the authoritative projection owner.

`UnifiedSearchPlan`, `ViewPlan`, and `GraphContextPlan` extend the same
vocabulary to human command twins. Root search keeps direct provider/store
construction so its text warnings and rendering remain unchanged. Search and
views truthfully declare the established generic store opener's live read-write
schema and migration access. Graph context stays distinct from
the read-only agent plan: it declares live-note reads and optional direct
compression, while the root command registry retains link-target apply
authority without inventing SQLite session ownership. `agent
code-symbol-context` remains an explicit full-runtime parity exemption until
source-snippet output is frozen.
