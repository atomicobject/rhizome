package notemeta

import (
	"context"
	"fmt"
	"sort"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// durableMetadataRowsProvider exposes materialized source rows even while an
// ownership transition has deliberately invalidated global metadata readiness.
// It is a reconciliation-only read seam, not a replacement for current reads.
type durableMetadataRowsProvider interface {
	DurableNoteMetadataRowsByPaths(context.Context, []string) (map[string]MetadataRow, error)
}

type durableNoteFactsProvider interface {
	DurableNoteFactsByPaths(context.Context, []string) (semdb.DurableNoteFacts, error)
}

func canonicalPublishedNotePaths(notePaths []paths.NotePath) ([]string, error) {
	pathsList := make([]string, 0, len(notePaths))
	seen := make(map[string]struct{}, len(notePaths))
	for _, notePath := range notePaths {
		canonical, err := paths.CleanNotePath(notePath.String())
		if err != nil || canonical != notePath {
			return nil, fmt.Errorf("published note path %q must be canonical and vault-relative", notePath)
		}
		if _, duplicate := seen[canonical.String()]; duplicate {
			return nil, fmt.Errorf("published note path %q is repeated", canonical)
		}
		seen[canonical.String()] = struct{}{}
		pathsList = append(pathsList, canonical.String())
	}
	sort.Strings(pathsList)
	return pathsList, nil
}

func canonicalStoredNotePaths(rawPaths []string) ([]string, error) {
	notePaths := make([]paths.NotePath, len(rawPaths))
	for index, rawPath := range rawPaths {
		notePaths[index] = paths.NotePath(rawPath)
	}
	return canonicalPublishedNotePaths(notePaths)
}
