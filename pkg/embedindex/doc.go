// Package embedindex plans and executes note, code, and ontology embedding work.
//
// Planners compare source hashes before provider calls. Prepared pipelines and
// compatible runtime lanes pack provider work while application composition
// supplies providers and producer-owned write ports. Durable publication uses
// the application's single queued writer and its explicit flush barriers.
// Query execution belongs to search/semantic and does not import this package.
package embedindex
