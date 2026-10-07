package readmodel

import "context"

// ShapeSnapshot is a single read-only index snapshot for scope aggregates.
// It carries authored note membership, never fallback or embedded catalog types.
type ShapeSnapshot struct {
	SchemaHash string
	Rebuilding bool
	Notes      []ShapeNote
	Fields     []ShapeField
	Edges      []ShapeEdge
}
type ShapeNote struct {
	Path, Type, NodeID, Title string
	AssessmentJSON            string
	Changed                   int64
	HasIssues, Ambiguous      bool
}
type ShapeField struct{ Path, Field, Value string }
type ShapeEdge struct {
	Source, Target, SourceNode, TargetNode, Field string
	Relation                                      bool
}
type ShapeStore interface {
	OntologyShapeSnapshot(context.Context, []string, bool, int) (ShapeSnapshot, error)
}

type RecentNotes struct {
	Notes []ShapeNote
	Count int
}

type RecentStore interface {
	RecentOntologyNotes(context.Context, int, int) (RecentNotes, error)
}
