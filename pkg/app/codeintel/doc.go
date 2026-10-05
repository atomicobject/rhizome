// Package codeintel provides command-facing orchestration for code-intel ingest.
//
// This package keeps cmd thin while coordinating filesystem discovery, vault
// include/ignore policy, mtime/hash skipping, worker fanout, batched writes, and
// optional semantic submission. The durable index model lives in pkg/anchors;
// codeintel should pass vault-root-relative paths and prepared work across that
// boundary rather than inventing separate path or identity rules.
//
// The main performance invariant is that file workers do parsing and local work,
// then hand off to writer/semantic lanes. Waiting for SQLite drains or embedding
// provider backpressure inside every worker makes index_code wall time scale with
// the slowest external sink instead of parse throughput.
package codeintel
