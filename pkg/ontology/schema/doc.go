// Package schema compiles ontology SDL into Rhizome's immutable typed model.
//
// It owns selectors over provider-current Subject facts, scalar parsing,
// identifier strategies, presentation profiles, and field capabilities.
// The Obsidian dependency supplies fuzzy matching and property analysis only.
// Projection, validation diagnostics, indexed rows, and publication belong to
// the parent ontology package and its consumers.
package schema
