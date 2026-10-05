package semantic

import (
	"context"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

func NewOntologyRawNoteEligibility(store *semdb.Store) RawNoteEmbeddingEligibilitySource {
	return ontologyRawNoteEligibility{store: store}
}

type ontologyRawNoteEligibility struct {
	store *semdb.Store
}

func (e ontologyRawNoteEligibility) RawNoteEmbeddingEligibility(ctx context.Context, paths []string) (RawNoteEmbeddingEligibility, error) {
	out := RawNoteEmbeddingEligibility{
		RawEligible: make(map[string]bool, len(paths)),
	}
	for _, path := range paths {
		out.RawEligible[path] = true
	}
	if e.store == nil || len(paths) == 0 {
		return out, nil
	}
	state, err := e.store.GetOntologySchemaState(ctx)
	if err != nil {
		return RawNoteEmbeddingEligibility{}, err
	}
	if !state.Ready || strings.TrimSpace(state.SchemaHash) == "" || state.MaterializationVersion != ontology.OntologyMaterializationVersion {
		return out, nil
	}
	rows, err := e.store.OntologyAssessmentsByPaths(ctx, paths)
	if err != nil {
		return RawNoteEmbeddingEligibility{}, err
	}
	for _, path := range paths {
		out.RawEligible[path] = false
		row, ok := rows[path]
		if !ok {
			out.TypedPaths = append(out.TypedPaths, path)
			continue
		}
		if strings.TrimSpace(row.ResolvedType) != "" || strings.TrimSpace(row.AssessmentJSON) != "" {
			out.TypedPaths = append(out.TypedPaths, path)
		}
	}
	return out, nil
}
