package search

import (
	"context"
	"path"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// CorpusSnapshot holds the catalog shared by every facet of one search request.
// Callers build it once, before concurrent planning and retrieval.
type CorpusSnapshot struct {
	Notes     []semdb.NoteMetadataRow
	NotePaths []string
	CodePaths []string
	PathKinds map[string]PathKind
}

// CorpusDetail selects the data a request actually consumes.
type CorpusDetail uint8

const (
	CorpusPaths     CorpusDetail = iota // Counts and note-path retrieval only.
	CorpusOwnership                     // Current projected note ownership, without metadata hydration.
	CorpusMetadata                      // Authoritative note metadata and ownership.
)

func LoadCorpusSnapshot(ctx context.Context, store *semdb.Store, detail CorpusDetail) (*CorpusSnapshot, error) {
	snapshot := &CorpusSnapshot{}
	if detail >= CorpusOwnership {
		snapshot.PathKinds = make(map[string]PathKind)
	}
	if store == nil {
		return snapshot, nil
	}
	var err error
	if detail == CorpusMetadata {
		snapshot.Notes, err = store.CurrentNoteMetadataRows(ctx)
		if err != nil {
			return nil, err
		}
	}
	if detail != CorpusOwnership {
		snapshot.NotePaths, err = store.IntelNotePaths(ctx)
		if err != nil {
			return nil, err
		}
	}
	snapshot.CodePaths, err = store.IndexedFilePaths(ctx)
	if err != nil {
		return nil, err
	}
	add := func(raw string, kind PathKind) {
		raw = NormalizeLocalityPath(raw)
		if raw == "" {
			return
		}
		for current := raw; current != "." && current != ""; current = path.Dir(current) {
			if _, exists := snapshot.PathKinds[current]; !exists {
				snapshot.PathKinds[current] = kind
			}
		}
	}
	for _, note := range snapshot.Notes {
		if note.Projection.Status == semdb.NoteProjectionStatusCurrent {
			add(note.Path, PathKindNote)
		}
	}
	if detail == CorpusOwnership {
		paths, err := store.SearchNotePaths(ctx)
		if err != nil {
			return nil, err
		}
		for _, note := range paths {
			if note.HasSections {
				snapshot.NotePaths = append(snapshot.NotePaths, note.Path)
			}
			if note.CurrentProjection {
				add(note.Path, PathKindNote)
			}
		}
	}
	if detail >= CorpusOwnership {
		for _, code := range snapshot.CodePaths {
			add(code, PathKindCode)
		}
	}
	return snapshot, nil
}
