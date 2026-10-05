// Package indexing owns the durable batch indexing pipeline behind rzm index.
//
// The package is intentionally a coordinator, not a bag of independent jobs:
// code ingest, note ingest, ontology projection, semantic embedding, generated
// primary-chunk sync, graph maintenance, and SQLite maintenance share the same timing
// collector, one queued writer lane, and explicit correctness barriers. Keep
// expensive provider work parallel, but keep durable SQLite writes serialized
// through the queue so contention shows up as backpressure instead of retry
// storms.
package indexing
