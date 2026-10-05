package ontology

const (
	TypeScopeAll    = "__all__"
	TypeScopeIssues = "__issues__"
)

// NodeListItem is a lightweight ontology instance row keyed by canonical NodeRef.
type NodeListItem struct {
	Ref          NodeRef
	Title        string
	ResolvedType string
	NotePath     string
	UpdatedAt    int64
	HasIssues    bool
}

// TypeListResult is a lightweight list/count payload for one ontology type scope.
type TypeListResult struct {
	TypeDoc    *TypeDoc
	Count      int
	IssueCount int
	Items      []NodeListItem
}
