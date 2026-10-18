package store

import (
	"strings"
)

type OntologyNoteTypeRow struct {
	NotePath   string
	TypeName   string
	SchemaHash string
	UpdatedAt  int64
}

type OntologyEdgeRow struct {
	SrcPath      string
	SrcNodeID    string
	RelationName string
	DstPath      string
	DstNodeID    string
	DstType      string
	Provenance   string
	Structural   bool
	SchemaHash   string
	UpdatedAt    int64
}

type OntologyEdgeKind string

const (
	OntologyEdgeKindField    OntologyEdgeKind = "field"
	OntologyEdgeKindBodyLink OntologyEdgeKind = "body_link"
	OntologyEdgeKindBacklink OntologyEdgeKind = "backlink"
	OntologyEdgeKindUnknown  OntologyEdgeKind = "unknown"
)

func (k OntologyEdgeKind) IsStructural() bool {
	return k == OntologyEdgeKindField
}

func (r OntologyEdgeRow) Kind() OntologyEdgeKind {
	if r.Structural {
		return OntologyEdgeKindField
	}
	switch strings.ToLower(strings.TrimSpace(r.Provenance)) {
	case "body_link":
		return OntologyEdgeKindBodyLink
	case "backlink":
		return OntologyEdgeKindBacklink
	default:
		return OntologyEdgeKindUnknown
	}
}

type OntologyTypePolicyRow struct {
	TypeName   string
	PolicyJSON string
	SchemaHash string
	UpdatedAt  int64
}

type OntologyNoteAssessmentRow struct {
	NotePath       string
	DeclaredType   string
	ResolvedType   string
	AssessmentJSON string
	HasIssues      bool
	TypeAmbiguous  bool
	SchemaHash     string
	UpdatedAt      int64
}

// OntologyAssessmentFlags are the inventory-level facts materialized next to
// each assessment row so whole-vault reads never decode assessment JSON.
type OntologyAssessmentFlags struct {
	HasIssues     bool
	TypeAmbiguous bool
}

type OntologySchemaState struct {
	SchemaHash             string
	NotesHash              string
	MaterializationVersion int
	LoadedAt               int64
	Ready                  bool
	ErrorJSON              string
}

type OntologySnapshot struct {
	Assessments  []OntologyNoteAssessmentRow
	NoteStates   []OntologyNoteStateRow
	NoteTypes    []OntologyNoteTypeRow
	Edges        []OntologyEdgeRow
	TypePolicies []OntologyTypePolicyRow
	SchemaState  OntologySchemaState
}

const (
	ValidationStatusNeverRan = "never_ran"
	ValidationStatusRunning  = "running"
	ValidationStatusOK       = "ok"
	ValidationStatusError    = "error"
)

type ValidationState struct {
	Status              string
	ResultJSON          string
	Error               string
	Generation          int64
	PublishedGeneration int64
	StartedAt           int64
	FinishedAt          int64
	DurationMs          int64
}
