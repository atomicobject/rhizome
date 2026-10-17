package ontology

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOpenPublishedRuntimeDoesNotCreateAnIndex(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `type Record @node(paths: ["notes/*.md"]) { title: String }`)
	before, err := os.ReadDir(filepath.Join(root, ".rhizome"))
	require.NoError(t, err)
	runtime, close, err := OpenPublishedRuntime(context.Background(), testkit.NoteMetadataIndexer(t), obsidian.VaultDefinition{Path: root})
	require.ErrorContains(t, err, "run `rzm index`")
	require.Nil(t, close)
	require.False(t, runtime.Ready)
	after, err := os.ReadDir(filepath.Join(root, ".rhizome"))
	require.NoError(t, err)
	var oldNames, newNames []string
	for _, entry := range before {
		oldNames = append(oldNames, entry.Name())
	}
	for _, entry := range after {
		newNames = append(newNames, entry.Name())
	}
	require.Equal(t, oldNames, newNames)
}
