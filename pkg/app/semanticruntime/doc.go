// Package semanticruntime groups compatible embedding work onto shared provider
// lanes.
//
// Full scans, watcher bursts, ontology primary-chunk sync, and intent
// exemplar sync can all hit the same embedding provider. The runtime decides
// when those requests are compatible enough to share one node so provider
// packing, dedupe, concurrency caps, and timings remain coherent across the
// indexing pipeline.
package semanticruntime
