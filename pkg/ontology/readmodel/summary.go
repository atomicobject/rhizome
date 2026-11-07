package readmodel

import "context"

// SummaryRow contains only the indexed facts needed to count and rank a type's examples.
type SummaryRow struct {
	Node          NodeRow
	MetadataTitle string
	MetadataMtime int64
	HasIssues     bool
}

type SummaryTotals struct {
	Notes, Typed, Issues, Ambiguous int
}

type SummaryStore interface {
	VisitOntologySummaryRows(context.Context, []string, int, func(SummaryRow) error) (bool, error)
	OntologySummaryTotals(context.Context, int) (SummaryTotals, bool, error)
}
