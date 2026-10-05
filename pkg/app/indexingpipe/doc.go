// Package indexingpipe provides the bounded file discovery and read stage used
// by the indexing pipeline.
//
// Discovery intentionally feeds a real worker queue instead of collecting every
// candidate first. That shape preserves backpressure, keeps memory bounded on
// large vaults, and makes queue-wait timings describe worker pressure rather
// than an artificial in-memory inventory.
package indexingpipe
