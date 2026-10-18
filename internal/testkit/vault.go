// Package testkit provides shared fixtures through the production note providers.
package testkit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
)

// Vault creates an isolated vault with authored files and a default configuration.
func Vault(t testing.TB, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, provided := files[".rhizome/config.yml"]; !provided {
		write(filepath.Join(root, ".rhizome/config.yml"), "notes: {}\n")
	}
	for path, content := range files {
		if filepath.IsAbs(path) || !filepath.IsLocal(path) {
			t.Fatalf("fixture path must be local: %q", path)
		}
		write(filepath.Join(root, path), content)
	}
	return root
}

func NoteMetadataIndexer(t testing.TB) notemeta.Indexer {
	t.Helper()
	runtime, err := builtin.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	indexer, err := notemeta.NewIndexer(runtime)
	if err != nil {
		t.Fatal(err)
	}
	return indexer
}

func NoteSource(t testing.TB, path, content string, mtime int64) notemeta.NoteSourceSnapshot {
	t.Helper()
	source, err := NoteMetadataIndexer(t).ProjectNoteSource(path, content, mtime)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

// DescriptorOnlyHTMLNoteMetadataIndexer advertises HTML without a projector.
func DescriptorOnlyHTMLNoteMetadataIndexer(t testing.TB) notemeta.Indexer {
	t.Helper()
	registry, err := builtin.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := noteformat.NewRuntime(registry, markdown.New())
	if err != nil {
		t.Fatal(err)
	}
	indexer, err := notemeta.NewIndexer(runtime)
	if err != nil {
		t.Fatal(err)
	}
	return indexer
}
