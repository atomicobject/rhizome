package notemeta

import (
	"context"
	"sync"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type sourceCaptureKey struct {
	path  paths.NotePath
	hash  string
	mtime int64
}
type capturedProjection struct {
	source  noteformat.AuthoredSource
	project func() (noteformat.Projection, error)
}

// WithSourceScope returns an operation-local indexer. Publication can reuse its
// sealed captures after the metadata barrier; a new operation gets a new scope.
func (i Indexer) WithSourceScope() Indexer {
	i.sources = &sync.Map{}
	return i
}

func (i Indexer) projectSource(source noteformat.AuthoredSource) (projectedNoteEntry, error) {
	project := func() (noteformat.Projection, error) { return i.formats.Project(source) }
	if i.sources != nil {
		key := sourceCaptureKey{source.Path(), source.ContentHash(), source.Mtime()}
		captured, _ := i.sources.LoadOrStore(key, capturedProjection{source, sync.OnceValues(project)})
		project = captured.(capturedProjection).project
	}
	projection, err := project()
	if err != nil {
		return projectedNoteEntry{}, err
	}
	return projectionEntryFrom(source, projection)
}

func (i Indexer) loadPublishedEntries(ctx context.Context, vault obsidian.VaultDefinition, reader obsidian.NoteReader, pathsList []string, rows map[string]semdb.NoteMetadataRow) ([]projectedNoteEntry, error) {
	return loadBounded(ctx, len(pathsList), func(index int) (projectedNoteEntry, error) {
		path := pathsList[index]
		row := rows[path]
		if i.sources != nil {
			key := sourceCaptureKey{paths.NotePath(path), row.ContentHash, row.Mtime}
			if value, ok := i.sources.Load(key); ok {
				captured := value.(capturedProjection)
				if string(captured.source.Format()) == row.FormatID && captured.source.Size() == row.Size {
					return i.projectSource(captured.source)
				}
			}
		}
		return i.readProjectedEntry(vault, reader, path)
	})
}
