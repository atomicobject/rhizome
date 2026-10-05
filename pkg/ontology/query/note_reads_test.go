package query

import (
	"context"
	"fmt"
	"sync"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type boundedQueryReader struct {
	obsidian.NoteReader
	contents, lists int
}

func (r *boundedQueryReader) GetContents(v obsidian.VaultDefinition, path string) (string, error) {
	r.contents++
	return r.NoteReader.GetContents(v, path)
}
func (r *boundedQueryReader) GetNotesList(v obsidian.VaultDefinition) ([]string, error) {
	r.lists++
	return r.NoteReader.GetNotesList(v)
}

type boundedQueryStore struct {
	*semdb.Store
	allMetadataReads int
}

type inventoryQueryReader struct {
	obsidian.NoteReader
	mu       sync.Mutex
	paths    []string
	contents map[string]int
	lists    int
}

func (r *inventoryQueryReader) GetContents(v obsidian.VaultDefinition, path string) (string, error) {
	r.mu.Lock()
	r.contents[path]++
	r.mu.Unlock()
	return r.NoteReader.GetContents(v, path)
}

func (r *inventoryQueryReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lists++
	return append([]string(nil), r.paths...), nil
}

type snapshotInventoryQueryReader struct {
	*inventoryQueryReader
	entries []obsidian.NoteEntry
}

func (r *snapshotInventoryQueryReader) NoteEntriesSnapshot(context.Context) ([]obsidian.NoteEntry, error) {
	return append([]obsidian.NoteEntry(nil), r.entries...), nil
}

type currentMetadataQueryStore struct {
	*semdb.Store
	rows    []semdb.NoteMetadataRow
	aliases map[string][]string
}

func (s *currentMetadataQueryStore) CurrentNoteMetadataRows(context.Context) ([]semdb.NoteMetadataRow, error) {
	return append([]semdb.NoteMetadataRow(nil), s.rows...), nil
}

func (s *currentMetadataQueryStore) CurrentNoteAliases(context.Context) (map[string][]string, error) {
	return s.aliases, nil
}

func (s *boundedQueryStore) CurrentNoteMetadataRows(ctx context.Context) ([]semdb.NoteMetadataRow, error) {
	s.allMetadataReads++
	return s.Store.CurrentNoteMetadataRows(ctx)
}

func TestExecuteExplicitNotePathsDoNotReadUnrelatedNotes(t *testing.T) {
	notes := map[string]string{}
	for i := 0; i < 20; i++ {
		notes[fmt.Sprintf("notes/%04d.md", i)] = fmt.Sprintf("---\nname: Item %d\n---\nBody\n", i)
	}
	env := newCustomQueryTestEnv(t, `type Item @node(paths: ["notes/*.md"]) { name: String }`, notes)
	reader := &boundedQueryReader{NoteReader: &obsidian.Note{}}
	store := &boundedQueryStore{Store: env.store}
	deps := env.deps(nil)
	deps.NoteReader, deps.Store = reader, store
	prepared, errs := Prepare(env.execSchema, `{
  found: note(path: "notes/0000.md") { path title }
  missing: note(path: "notes/missing.md") { path title }
  repeated: note(path: "notes/0000.md") { path title }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, "notes/0000.md", result.Data["found"].(map[string]any)["path"])
	require.Nil(t, result.Data["missing"])
	require.Equal(t, result.Data["found"], result.Data["repeated"])
	require.Zero(t, reader.contents, "summary and missing-path reads must not parse unrelated notes")
	require.Zero(t, reader.lists, "explicit paths do not need a shortest-wikilink inventory")
	require.Zero(t, store.allMetadataReads, "tiny path reads must not load whole-vault metadata")
}

func TestExecuteWorkspaceSourceLinksUsesCurrentInventoryWithoutReadingUnrelatedContent(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Source @node(paths: ["notes/source.md"]) { label: String @field }`, map[string]string{
		"notes/source.md":     "[[fresh-alias]] [[new-target]] [[stale]] [[html]] [[deleted]] [[deleted-alias]]\n",
		"notes/alias-a.md":    "# Alias A\n",
		"notes/alias-b.md":    "# Alias B\n",
		"notes/new-target.md": "# New target\n",
		"notes/stale.md":      "# Stale\n",
		"notes/html.md":       "# HTML\n",
		"notes/deleted.md":    "# Deleted\n",
		"notes/unrelated.md":  "# Unrelated\n",
	})
	rows, err := env.store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	for i := range rows {
		switch rows[i].Path {
		case "notes/stale.md":
			rows[i].Projection.Status = semdb.NoteProjectionStatusStale
		case "notes/html.md":
			rows[i].FormatID = "html"
		}
	}
	store := &currentMetadataQueryStore{
		Store:   env.store,
		rows:    rows,
		aliases: map[string][]string{"notes/alias-a.md": {"fresh-alias"}, "notes/deleted.md": {"deleted-alias"}},
	}
	reader := &inventoryQueryReader{
		NoteReader: &obsidian.Note{},
		paths:      []string{"notes/source.md", "notes/alias-a.md", "notes/alias-b.md", "notes/stale.md", "notes/html.md", "notes/unrelated.md"},
		contents:   map[string]int{},
	}
	deps := env.deps(nil)
	deps.NoteReader, deps.Store = reader, store
	prepared, errs := PrepareWithVariables(env.execSchema, `query($path: String!) {
  node(ref: $path) { workspace { sourceLinks(first: 20) {
    target authoredTarget text kind anchor embed targetKind resolved resolvedRef { ref kind notePath fragment } title preview
  } } }
}`, map[string]any{"path": "notes/source.md"})
	require.Empty(t, errs)

	first := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, first.Errors)
	require.Equal(t, map[string]int{"notes/source.md": 1}, reader.contents, "only the requested source payload may read content")
	firstLinks := first.Data["node"].(map[string]any)["workspace"].(map[string]any)["sourceLinks"].([]any)
	require.Len(t, firstLinks, 6)
	require.Equal(t, "notes/alias-a.md", firstLinks[0].(map[string]any)["target"])
	require.Equal(t, true, firstLinks[0].(map[string]any)["resolved"])
	for _, index := range []int{1, 2, 3, 4, 5} {
		require.Equal(t, false, firstLinks[index].(map[string]any)["resolved"])
	}

	store.aliases = map[string][]string{"notes/alias-b.md": {"fresh-alias"}, "notes/deleted.md": {"deleted-alias"}}
	reader.paths = append(reader.paths, "notes/new-target.md")
	second := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, second.Errors)
	secondLinks := second.Data["node"].(map[string]any)["workspace"].(map[string]any)["sourceLinks"].([]any)
	require.Equal(t, "notes/alias-b.md", secondLinks[0].(map[string]any)["target"], "aliases are refreshed for each execution")
	require.Equal(t, "notes/new-target.md", secondLinks[1].(map[string]any)["target"], "the live path inventory is refreshed for each execution")
	require.Equal(t, true, secondLinks[1].(map[string]any)["resolved"])
	require.Equal(t, map[string]int{"notes/source.md": 2}, reader.contents, "refreshing the resolution inventory must not read target or unrelated content")
}

func TestCurrentNotePathsPrefersReaderSnapshot(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Source @node(paths: ["notes/source.md"]) { label: String @field }`, map[string]string{
		"notes/source.md":  "# Source\n",
		"notes/current.md": "# Current\n",
	})
	rows, err := env.store.CurrentNoteMetadataRows(context.Background())
	require.NoError(t, err)
	store := &currentMetadataQueryStore{Store: env.store, rows: rows}
	base := &inventoryQueryReader{NoteReader: &obsidian.Note{}, paths: []string{"notes/wrong.md"}, contents: map[string]int{}}
	reader := &snapshotInventoryQueryReader{
		inventoryQueryReader: base,
		entries:              []obsidian.NoteEntry{{Path: "notes/current.md"}, {Path: "notes/missing.md"}},
	}
	deps := env.deps(nil)
	deps.NoteReader, deps.Store = reader, store

	paths, err := currentNotePaths(context.Background(), deps)
	require.NoError(t, err)
	require.Equal(t, []string{"notes/current.md"}, paths)
	require.Zero(t, base.lists, "snapshot-backed readers do not need a separate vault listing")
	require.Empty(t, base.contents)
}
