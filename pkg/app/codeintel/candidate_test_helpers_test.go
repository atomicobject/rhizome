package codeintel

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	anchors "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/indexwriter"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/stretchr/testify/require"
)

func newCandidateTestWriter(t testing.TB, service *anchors.Service) *indexwriter.Writer {
	t.Helper()
	writer := indexwriter.New(context.Background(), indexwriter.Handlers{ApplyCodeIndexBatch: service.ApplyCodeIndexBatch, ApplyNoteIndexBatch: service.ApplyNoteIndexBatch})
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	return writer
}
func newCandidateTestIndexer(t testing.TB) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)
	return indexer
}
func codeCandidatesForTest(t testing.TB, vault, root string, service *anchors.Service) []FileCandidate {
	t.Helper()
	var candidates []FileCandidate
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		lang := DetectCodeLang(path)
		if lang == "" || !service.HasIndexer(lang) {
			return nil
		}
		rel, err := filepath.Rel(vault, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		candidates = append(candidates, FileCandidate{AbsPath: path, RelPath: filepath.ToSlash(rel), Kind: FileKindCode, Lang: lang, ModTime: info.ModTime().Unix()})
		return nil
	}))
	return candidates
}
