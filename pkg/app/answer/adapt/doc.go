// Package adapt converts search and ontology provenance into
// pkg/app/answer DTOs.
//
// Keep this package pure: no store reads, no semantic hydration, no ontology
// projection, and no role/confidence decisions. Callers should resolve all
// provenance before entering adapt; pkg/app/answer owns packet shaping.
package adapt
