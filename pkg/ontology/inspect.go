package ontology

import (
	"context"
	"sort"
	"strings"
)

type InspectRelation struct {
	RelationName    string `json:"relationName"`
	SourcePath      string `json:"sourcePath"`
	DestinationPath string `json:"destinationPath"`
	DestinationType string `json:"destinationType,omitempty"`
	Provenance      string `json:"provenance,omitempty"`
	Structural      bool   `json:"structural"`
}

type InspectNote struct {
	Path               string            `json:"path"`
	ResolvedType       string            `json:"resolvedType,omitempty"`
	Assessment         *NoteAssessment   `json:"assessment,omitempty"`
	TypeDoc            *TypeDoc          `json:"typeDoc,omitempty"`
	PersistedRelations []InspectRelation `json:"persistedRelations,omitempty"`
}

type InspectResult struct {
	OntologyAvailable bool          `json:"ontologyAvailable"`
	SchemaHash        string        `json:"schemaHash,omitempty"`
	Notes             []InspectNote `json:"notes"`
}

func (s *Service) Inspect(ctx context.Context, notePath string) (InspectNote, error) {
	out := InspectNote{Path: strings.TrimSpace(notePath)}
	if out.Path == "" {
		return out, nil
	}

	assessment, ok, err := s.Assessment(ctx, out.Path)
	if err != nil {
		return out, err
	}
	if ok {
		out.Assessment = assessment
		out.ResolvedType = assessment.ResolvedType
	}
	if out.ResolvedType == "" {
		row, ok, err := s.ResolvedType(ctx, out.Path)
		if err != nil {
			return out, err
		}
		if ok {
			out.ResolvedType = row.TypeName
		}
	}
	if out.ResolvedType != "" {
		doc, err := s.TypeDoc(out.ResolvedType)
		if err != nil {
			return out, err
		}
		out.TypeDoc = doc
	}

	relations, err := s.Store.OntologyEdgesForPath(ctx, out.Path, true, "", 0)
	if err != nil {
		return out, err
	}
	if len(relations) > 0 {
		out.PersistedRelations = make([]InspectRelation, 0, len(relations))
		for _, relation := range relations {
			out.PersistedRelations = append(out.PersistedRelations, InspectRelation{
				RelationName:    relation.RelationName,
				SourcePath:      relation.SrcPath,
				DestinationPath: relation.DstPath,
				DestinationType: relation.DstType,
				Provenance:      relation.Provenance,
				Structural:      relation.Structural,
			})
		}
	}

	return out, nil
}

func (s *Service) InspectPaths(ctx context.Context, notePaths []string) (*InspectResult, error) {
	result := &InspectResult{
		OntologyAvailable: s != nil && s.Schema != nil,
		Notes:             []InspectNote{},
	}
	if s != nil && s.Schema != nil {
		result.SchemaHash = s.Schema.Hash
	}

	seen := make(map[string]struct{}, len(notePaths))
	ordered := make([]string, 0, len(notePaths))
	for _, notePath := range notePaths {
		notePath = strings.TrimSpace(notePath)
		if notePath == "" {
			continue
		}
		if _, ok := seen[notePath]; ok {
			continue
		}
		seen[notePath] = struct{}{}
		ordered = append(ordered, notePath)
	}
	sort.Strings(ordered)

	result.Notes = make([]InspectNote, 0, len(ordered))
	for _, notePath := range ordered {
		inspected, err := s.Inspect(ctx, notePath)
		if err != nil {
			return nil, err
		}
		result.Notes = append(result.Notes, inspected)
	}
	return result, nil
}
