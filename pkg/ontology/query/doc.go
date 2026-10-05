// Package query exposes a bounded read-only GraphQL view over the compiled
// ontology.
//
// Execution is deliberately not a second ontology runtime. Roots must be
// bounded by path/find/property/semantic selectors, and relation reads batch
// through loaders and noderead scopes. The search root runs the shared unified
// engine restricted to notes; semantic: selectors stay chunk-only and are never
// an implicit fallback for unresolved queries.
package query
