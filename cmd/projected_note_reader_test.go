package cmd

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNewProjectedNoteReaderDoesNotCreateOrWriteIntelStore(t *testing.T) {
	for _, testCase := range []struct {
		name                 string
		makeIndexDirReadOnly bool
	}{
		{name: "absent index"},
		{name: "unwritable index", makeIndexDirReadOnly: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "Notes"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "Notes", "Decision.MD"), []byte("# Decision\n"), 0o644))
			if testCase.makeIndexDirReadOnly {
				indexDir := filepath.Join(root, ".rhizome")
				require.NoError(t, os.Mkdir(indexDir, 0o500))
				t.Cleanup(func() { require.NoError(t, os.Chmod(indexDir, 0o755)) })
			}

			reader, err := newProjectedNoteReader(context.Background(), obsidian.VaultDefinition{Path: root})
			require.NoError(t, err)
			require.Equal(t, []string{"Notes/Decision.MD"}, reader.paths)
			_, err = os.Stat(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}
}

func TestNewProjectedNoteReaderUsesOneSourcePassForBuiltinFormats(t *testing.T) {
	source := &countingProjectedSource{
		notes: []string{"Notes/Decision.MD", "Notes/Reference.html"},
		contents: map[string]string{
			"Notes/Decision.MD":    "---\ntags: [decision]\n---\n# Decision\n",
			"Notes/Reference.html": "<h1>Reference</h1>\n",
		},
		reads: make(map[string]int),
		stats: make(map[string]int),
	}
	indexer, err := newNoteMetadataIndexer()
	require.NoError(t, err)

	reader, err := newProjectedNoteReaderWithSource(context.Background(), obsidian.VaultDefinition{Path: t.TempDir()}, indexer, source)
	require.NoError(t, err)
	require.Equal(t, []string{"Notes/Decision.MD", "Notes/Reference.html"}, reader.paths)
	require.Equal(t, 1, source.listCalls)
	require.Equal(t, map[string]int{"Notes/Decision.MD": 1, "Notes/Reference.html": 1}, source.reads)
	require.Equal(t, map[string]int{"Notes/Decision.MD": 1, "Notes/Reference.html": 1}, source.stats)
	fact, ok := reader.NoteFacts().LookupFact("Notes/Decision.MD")
	require.True(t, ok)
	require.Equal(t, []string{"decision"}, fact.Tags)
	htmlFact, ok := reader.NoteFacts().LookupFact("Notes/Reference.html")
	require.True(t, ok)
	require.Equal(t, noteformat.FormatID("html"), htmlFact.Format)
	title, ok := reader.Title("Notes/Reference.html")
	require.True(t, ok)
	require.Equal(t, "Reference", title)
}

func TestNewProjectedNoteReaderOmitsDescriptorOnlyHTML(t *testing.T) {
	source := &countingProjectedSource{
		notes: []string{"Notes/Decision.MD", "Notes/Reference.html"},
		contents: map[string]string{
			"Notes/Decision.MD":    "# Decision\n",
			"Notes/Reference.html": "<h1>Reference</h1>\n",
		},
		reads: make(map[string]int),
		stats: make(map[string]int),
	}
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New())
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(t, err)

	reader, err := newProjectedNoteReaderWithSource(context.Background(), obsidian.VaultDefinition{Path: t.TempDir()}, indexer, source)
	require.NoError(t, err)
	require.Equal(t, []string{"Notes/Decision.MD"}, reader.paths)
	require.Equal(t, 1, source.listCalls)
	require.Equal(t, map[string]int{"Notes/Decision.MD": 1}, source.reads)
	require.Equal(t, map[string]int{"Notes/Decision.MD": 1}, source.stats)
}

type countingProjectedSource struct {
	mu        sync.Mutex
	notes     []string
	contents  map[string]string
	listCalls int
	reads     map[string]int
	stats     map[string]int
}

func (r *countingProjectedSource) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads[path]++
	return r.contents[path], nil
}

func (r *countingProjectedSource) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	return append([]string(nil), r.notes...), nil
}

func (r *countingProjectedSource) GetModTime(_ obsidian.VaultDefinition, path string) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats[path]++
	return time.Unix(0, 0), nil
}

func (r *countingProjectedSource) Title(string) (string, bool) { return "", false }

var _ obsidian.NoteReader = (*countingProjectedSource)(nil)
