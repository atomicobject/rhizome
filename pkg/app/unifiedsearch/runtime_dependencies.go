package unifiedsearch

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"slices"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/store"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/search"
)

func loadOntologySchemaBestEffort(vaultPath string) *ontoschema.Schema {
	schema, err := ontoschema.LoadSchema(vaultPath)
	if err != nil {
		return nil
	}
	return schema
}

func providerConfigsMatch(a, b embeddings.ProviderConfig) bool {
	return strings.EqualFold(strings.TrimSpace(a.Provider), strings.TrimSpace(b.Provider)) &&
		strings.TrimSpace(a.Model) == strings.TrimSpace(b.Model) &&
		strings.TrimSpace(a.Endpoint) == strings.TrimSpace(b.Endpoint) &&
		a.Dimensions == b.Dimensions &&
		strings.TrimSpace(a.APIKey) == strings.TrimSpace(b.APIKey)
}

// filterResultsByNoteType drops note results whose owning note type is not
// selected. Retrievers already honor NoteTypes; explicit seeds bypass them, so
// the owning-type check is applied once here for every lane.
func filterResultsByNoteType(ctx context.Context, store *semdb.Store, results []search.RankedResult, noteTypes []string) ([]search.RankedResult, error) {
	if store == nil || len(noteTypes) == 0 || len(results) == 0 {
		return results, nil
	}
	paths := make([]string, 0, len(results))
	for _, result := range results {
		if result.Type != "code" && strings.TrimSpace(result.Path) != "" {
			paths = append(paths, result.Path)
		}
	}
	rows, err := store.OntologyTypesByPaths(ctx, paths)
	if err != nil {
		// Fail closed: an unverifiable owning-note type must not widen a
		// noteType restriction.
		return nil, fmt.Errorf("resolve owning note types for noteType filter: %w", err)
	}
	out := make([]search.RankedResult, 0, len(results))
	for _, result := range results {
		if result.Type == "code" {
			out = append(out, result)
			continue
		}
		row, ok := rows[result.Path]
		if ok && slices.ContainsFunc(noteTypes, func(want string) bool { return strings.EqualFold(want, row.TypeName) }) {
			out = append(out, result)
		}
	}
	return out, nil
}
