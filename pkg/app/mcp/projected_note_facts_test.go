package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type noSourceReadFactsReader struct{ obsidian.Note }

func (noSourceReadFactsReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	panic("metadata enrichment read source bytes")
}
func (noSourceReadFactsReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	panic("metadata enrichment enumerated source files")
}
func (noSourceReadFactsReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	panic("metadata enrichment statted source file")
}

func TestProjectedNoteFactsUseLazyPersistedMetadataWithoutSourceReads(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := "---\nsummary: {text: nested, count: 3}\n---\n# Source\n#Planning"
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.md"), []byte(source), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "unrelated.md"), []byte("# Other"), 0o644))
	store := newIntelStore(t)
	indexer := configuredMCPNoteMetadata(t)
	vault := obsidian.VaultDefinition{Path: root}
	_, err := indexer.EnsureIndexed(ctx, vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	// The unrelated projection is valid JSON with the wrong payload shape. A
	// bounded lookup must not decode it while enriching source.md.
	_, err = store.DB().ExecContext(ctx, `UPDATE note_projection_payloads SET payload_json='[]' WHERE note_id=(SELECT id FROM notes WHERE path='unrelated.md')`)
	require.NoError(t, err)
	reader := withProjectedNoteFacts(ctx, Config{NoteMetadata: indexer, VaultDef: vault, IntelStore: store}, &noSourceReadFactsReader{})
	facts := actions.NoteFactsFromReader(reader)
	require.Equal(t, []string{"source.md", "unrelated.md"}, facts.Paths())
	first, ok := facts.LookupFact("source.md")
	require.True(t, ok)
	require.Equal(t, map[string]any{"text": "nested", "count": 3}, first.Frontmatter["summary"])
	require.Equal(t, []string{"Planning"}, first.Tags)
	require.Equal(t, source, first.Content)
	// Request-local cache retains one coherent projected result.
	_, err = store.DB().ExecContext(ctx, `UPDATE note_projection_payloads SET payload_json='[]' WHERE note_id=(SELECT id FROM notes WHERE path='source.md')`)
	require.NoError(t, err)
	second, ok := facts.LookupFact("source.md")
	require.True(t, ok)
	require.Equal(t, first, second)
}
