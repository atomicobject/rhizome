package cmd

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// projectedNoteReader is the one-shot CLI adapter for provider-current source
// facts. It deliberately exposes no Markdown parser behavior: descriptor-only
// sources are omitted before this adapter is built.
type projectedNoteReader struct {
	facts   actions.NoteFacts
	sources map[string]notemeta.NoteSourceSnapshot
	paths   []string
}

func newProjectedNoteReader(ctx context.Context, vaultDef obsidian.VaultDefinition) (*projectedNoteReader, error) {
	if vaultDef.BasePath() == "" {
		return nil, fmt.Errorf("vault path is required")
	}
	indexer, err := newNoteMetadataIndexer()
	if err != nil {
		return nil, err
	}
	reader, err := newProjectedNoteReaderWithSource(ctx, vaultDef, indexer, &obsidian.Note{})
	if err != nil {
		return nil, err
	}
	return reader, nil
}

// newProjectedNoteReaderWithSource creates a command-scoped, in-memory view
// from one provider projection pass. Read commands do not open, initialize, or
// refresh the durable Intel store.
func newProjectedNoteReaderWithSource(ctx context.Context, vaultDef obsidian.VaultDefinition, indexer notemeta.Indexer, source obsidian.NoteReader) (*projectedNoteReader, error) {
	runtime, err := indexer.FormatRuntime()
	if err != nil {
		return nil, err
	}
	notePaths, err := source.GetNotesList(vaultDef)
	if err != nil {
		return nil, err
	}
	projectablePaths := make([]string, 0, len(notePaths))
	for _, notePath := range notePaths {
		canonicalPath, err := paths.CleanNotePath(notePath)
		if err != nil {
			// Preserve the existing indexer diagnostic for malformed paths.
			projectablePaths = append(projectablePaths, notePath)
			continue
		}
		provider, known := runtime.ProviderForPath(paths.RelPath(canonicalPath))
		if known && !runtime.CanProject(provider.Descriptor().ID) {
			continue
		}
		projectablePaths = append(projectablePaths, canonicalPath.String())
	}

	sources, err := indexer.BuildNoteSourceSnapshots(ctx, vaultDef, listedNoteReader{
		NoteReader: source,
		paths:      projectablePaths,
	})
	if err != nil {
		return nil, err
	}
	reader := &projectedNoteReader{
		facts:   actions.NewNoteFacts(sources),
		sources: make(map[string]notemeta.NoteSourceSnapshot, len(sources)),
		paths:   make([]string, 0, len(sources)),
	}
	for _, source := range sources {
		path := paths.NormalizeNotePath(source.Path.String()).String()
		if path == "" {
			continue
		}
		reader.sources[path] = source
		reader.paths = append(reader.paths, path)
	}
	sort.Strings(reader.paths)
	return reader, nil
}

// listedNoteReader seals the command's single source discovery pass before
// provider projection reads content and metadata for those exact paths.
type listedNoteReader struct {
	obsidian.NoteReader
	paths []string
}

func (r listedNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return append([]string(nil), r.paths...), nil
}

func (r *projectedNoteReader) NoteFacts() actions.NoteFacts { return r.facts }

func (r *projectedNoteReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	source, ok := r.sources[paths.NormalizeNotePath(path).String()]
	if !ok {
		return "", fmt.Errorf("%s", obsidian.NoteDoesNotExistError)
	}
	return source.Content, nil
}

func (r *projectedNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return append([]string(nil), r.paths...), nil
}

func (r *projectedNoteReader) GetModTime(_ obsidian.VaultDefinition, path string) (time.Time, error) {
	source, ok := r.sources[paths.NormalizeNotePath(path).String()]
	if !ok {
		return time.Time{}, fmt.Errorf("%s", obsidian.NoteDoesNotExistError)
	}
	return time.Unix(source.Mtime, 0), nil
}

func (r *projectedNoteReader) Title(path string) (string, bool) {
	source, ok := r.sources[paths.NormalizeNotePath(path).String()]
	if !ok {
		return "", false
	}
	if title := strings.TrimSpace(source.Title); title != "" {
		return title, true
	}
	base := filepath.Base(source.Path.String())
	return strings.TrimSuffix(base, filepath.Ext(base)), true
}
