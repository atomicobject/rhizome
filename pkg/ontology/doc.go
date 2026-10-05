// Package ontology owns Rhizome's typed markdown contract.
//
// The package has four distinct jobs that intentionally meet at the markdown
// source boundary: compile GraphQL SDL into note and section types, project
// authored markdown into source-preserving NodeRefs, replay ontology edits as
// minimal span changes, and build indexed rows for query/search/graph
// consumers. Markdown remains the source of truth; SQLite rows and generated
// semantic chunks are read models that must converge back to the current schema and note
// fingerprints.
//
// Use noderead for request-scoped reads over indexed ontology state. Use
// ProjectNode/ProjectNodeFromSnapshot when a caller must inspect or edit one
// current file's authored spans.
package ontology
