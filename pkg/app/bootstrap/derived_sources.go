package bootstrap

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type publishedNoteReader struct {
	obsidian.NoteReader
	contents map[string]string
	mtimes   map[string]time.Time
}

func (r publishedNoteReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	value, ok := r.contents[path]
	if !ok {
		return "", fmt.Errorf("note source was not prepared")
	}
	return value, nil
}
func (r publishedNoteReader) GetModTime(_ obsidian.VaultDefinition, path string) (time.Time, error) {
	return r.mtimes[path], nil
}

func (w *unifiedSemanticWatcher) publishedNoteReader(ctx context.Context, paths []string) (obsidian.NoteReader, error) {
	rows, err := w.intelStore.CurrentNoteMetadataRowsByPaths(ctx, paths)
	if err != nil {
		return nil, err
	}
	reader := publishedNoteReader{NoteReader: &obsidian.Note{}, contents: map[string]string{}, mtimes: map[string]time.Time{}}
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(w.vaultPath, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != rows[path].ContentHash {
			return nil, errDerivedSuperseded
		}
		reader.contents[path] = string(data)
		reader.mtimes[path] = time.Unix(rows[path].Mtime, 0)
	}
	return reader, nil
}

func (w *unifiedSemanticWatcher) publishedCodeLoader(ctx context.Context, paths []string) (semantic.FileLoader, error) {
	sources := make(map[string][]byte, len(paths))
	for _, path := range paths {
		hash, _, _, present, err := w.intelStore.FileHash(ctx, path)
		if err != nil {
			return nil, err
		}
		if !present {
			continue
		}
		data, err := os.ReadFile(filepath.Join(w.vaultPath, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != hash {
			return nil, errDerivedSuperseded
		}
		sources[path] = data
	}
	return func(path string) ([]byte, error) {
		data, ok := sources[path]
		if !ok {
			return nil, fmt.Errorf("code source was not prepared")
		}
		return data, nil
	}, nil
}
