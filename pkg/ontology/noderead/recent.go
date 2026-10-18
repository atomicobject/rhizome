package noderead

import (
	"cmp"
	"context"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
)

// RecentNotes returns a bounded committed note inventory before hydration.
// Like TypeInstances(__all__), it includes otherwise-untyped note roots.
func (s *Scope) RecentNotes(ctx context.Context, limit int) (ontology.TypeListResult, error) {
	result := ontology.TypeListResult{TypeDoc: allNotesTypeDoc(), Items: []ontology.NodeListItem{}}
	if limit < 1 || limit > 500 {
		return result, fmt.Errorf("limit must be 1..500")
	}
	if s == nil || s.service == nil || s.service.Store == nil {
		return result, nil
	}
	if s.hasOverlay() {
		return result, fmt.Errorf("recent notes require a committed read scope")
	}
	store := s.service.Store
	recent, err := store.RecentOntologyNotes(ctx, limit, ontology.OntologyMaterializationVersion)
	if err != nil {
		return result, err
	}
	result.Count = recent.Count
	for _, n := range recent.Notes {
		if n.AssessmentJSON != "" {
			assessment, err := ontology.AssessmentFromJSON(n.AssessmentJSON)
			if err != nil {
				return result, err
			}
			flags := ontology.FlagsForAssessment(assessment)
			n.HasIssues, n.Ambiguous = flags.HasIssues, flags.TypeAmbiguous
		}
		typ := n.Type
		if n.Ambiguous || ontoschema.OntologyTypeVisibility(typ) == ontoschema.TypeVisibilityInternal {
			typ = ""
		}
		result.Items = append(result.Items, ontology.NodeListItem{Ref: ontology.NodeRef{NotePath: n.Path, Kind: ontology.NodeKindNote}, NotePath: n.Path, Title: cmp.Or(n.Title, n.Path), ResolvedType: typ, HasIssues: n.HasIssues, UpdatedAt: n.Changed})
		if n.HasIssues {
			result.IssueCount++
		}
	}
	return result, nil
}

func allNotesTypeDoc() *schemadoc.TypeDoc {
	return &schemadoc.TypeDoc{Name: ontology.TypeScopeAll, Label: "All notes", Description: "Every indexed note, regardless of resolved ontology type."}
}
