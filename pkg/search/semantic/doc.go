// Package semantic builds and queries the semantic chunk surfaces used by
// Rhizome search and indexing.
//
// The indexing side is hash-first: planners decide which note, ontology node,
// or code chunks changed before provider calls are made. Provider dispatch is
// packed through prepared pipelines and shared embedding nodes; durable writes
// are batched by the app/indexing writer queue when available. Keep those
// responsibilities separate: planners avoid provider work, embed pipelines keep
// providers full, and writers preserve SQLite backpressure.
package semantic
