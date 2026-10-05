package ontology

import (
	"encoding/json"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
)

type FieldAssessment struct {
	Name                string            `json:"name"`
	Description         string            `json:"description,omitempty"`
	Kind                FieldKind         `json:"kind"`
	TypeName            string            `json:"typeName"`
	Required            bool              `json:"required"`
	List                bool              `json:"list"`
	Source              string            `json:"source,omitempty"`
	SourceAliases       []string          `json:"sourceAliases,omitempty"`
	SourceKind          FieldSource       `json:"sourceKind,omitempty"`
	Semantics           SemanticsKind     `json:"semantics,omitempty"`
	Identifier          bool              `json:"identifier,omitempty"`
	PreferredIdentifier bool              `json:"preferredIdentifier,omitempty"`
	Present             bool              `json:"present"`
	Values              []string          `json:"values,omitempty"`
	ValidValues         []string          `json:"validValues,omitempty"`
	Issues              []ValidationIssue `json:"issues,omitempty"`
}

type RelationTarget struct {
	Path       string `json:"path"`
	TypeName   string `json:"typeName,omitempty"`
	Provenance string `json:"provenance,omitempty"`
	Structural bool   `json:"structural"`
}

type RelationAssessment struct {
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Kind          FieldKind         `json:"kind"`
	TypeName      string            `json:"typeName"`
	Required      bool              `json:"required"`
	List          bool              `json:"list"`
	Source        string            `json:"source,omitempty"`
	SourceAliases []string          `json:"sourceAliases,omitempty"`
	SourceKind    FieldSource       `json:"sourceKind,omitempty"`
	Direction     NeighborDirection `json:"direction,omitempty"`
	Semantics     SemanticsKind     `json:"semantics,omitempty"`
	Present       bool              `json:"present"`
	Values        []string          `json:"values,omitempty"`
	Targets       []RelationTarget  `json:"targets,omitempty"`
	Issues        []ValidationIssue `json:"issues,omitempty"`
}

type NoteAssessment struct {
	// Catalog witnesses record the completed projection, including intentional
	// emptiness. Missing count or digest is an incomplete materialization.
	CatalogNodeCount       *int                 `json:"catalogNodeCount,omitempty"`
	CatalogNodeDigest      string               `json:"catalogNodeDigest,omitempty"`
	SourceContentHash      string               `json:"sourceContentHash,omitempty"`
	NotePath               string               `json:"notePath"`
	DeclaredType           string               `json:"declaredType,omitempty"`
	ResolvedType           string               `json:"resolvedType,omitempty"`
	CandidateTypes         []string             `json:"candidateTypes,omitempty"`
	SelectorCandidateTypes []string             `json:"selectorCandidateTypes,omitempty"`
	Issues                 []ValidationIssue    `json:"issues,omitempty"`
	Fields                 []FieldAssessment    `json:"fields,omitempty"`
	Relations              []RelationAssessment `json:"relations,omitempty"`
}

func (a *NoteAssessment) Field(name string) (FieldAssessment, bool) {
	if a == nil {
		return FieldAssessment{}, false
	}
	for _, field := range a.Fields {
		if strings.EqualFold(field.Name, name) {
			return field, true
		}
	}
	return FieldAssessment{}, false
}

func (a *NoteAssessment) Relation(name string) (RelationAssessment, bool) {
	if a == nil {
		return RelationAssessment{}, false
	}
	for _, relation := range a.Relations {
		if strings.EqualFold(relation.Name, name) {
			return relation, true
		}
	}
	return RelationAssessment{}, false
}

// AssessmentHasIssues reports whether the note, any of its fields, or any of
// its relations carries a validation issue.
func AssessmentHasIssues(assessment *NoteAssessment) bool {
	if assessment == nil {
		return false
	}
	if len(assessment.Issues) > 0 {
		return true
	}
	for _, field := range assessment.Fields {
		if len(field.Issues) > 0 {
			return true
		}
	}
	for _, relation := range assessment.Relations {
		if len(relation.Issues) > 0 {
			return true
		}
	}
	return false
}

// FlagsForAssessment is the single source of truth for the inventory flags
// materialized alongside each assessment row.
func FlagsForAssessment(assessment *NoteAssessment) semdb.OntologyAssessmentFlags {
	if assessment == nil {
		return semdb.OntologyAssessmentFlags{}
	}
	return semdb.OntologyAssessmentFlags{
		HasIssues:     AssessmentHasIssues(assessment),
		TypeAmbiguous: len(assessment.CandidateTypes) > 1,
	}
}

// setAssessmentRow serializes the assessment into row and refreshes the
// materialized flags in the same call, so the columns and the JSON can never
// disagree.
func setAssessmentRow(row *semdb.OntologyNoteAssessmentRow, assessment NoteAssessment) {
	if row == nil {
		return
	}
	data, _ := json.Marshal(assessment)
	row.AssessmentJSON = string(data)
	flags := FlagsForAssessment(&assessment)
	row.HasIssues = flags.HasIssues
	row.TypeAmbiguous = flags.TypeAmbiguous
}

func decodeAssessment(raw string) (*NoteAssessment, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var assessment NoteAssessment
	if err := json.Unmarshal([]byte(raw), &assessment); err != nil {
		return nil, err
	}
	return &assessment, nil
}

func AssessmentFromJSON(raw string) (*NoteAssessment, error) {
	return decodeAssessment(raw)
}
