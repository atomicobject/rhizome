package idalloc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func indexedIdentifierPool(t *testing.T, sdl string, files map[string]string) (*ontology.Schema, *semdb.Store) {
	t.Helper()
	root := t.TempDir()
	files[".rhizome/ontology/schema.graphql"] = sdl
	files[".rhizome/config.yml"] = "vault:\n  notes:\n    include: ['**/*.md']\n"
	for name, content := range files {
		target := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, []byte(content), 0o644))
	}
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	formatRuntime, err := builtin.NewRuntime()
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(formatRuntime)
	require.NoError(t, err)
	vault := obsidian.VaultDefinition{Path: root}
	ctx := context.Background()
	_, err = indexer.EnsureIndexed(ctx, vault, &obsidian.Note{}, store)
	require.NoError(t, err)
	_, err = ontology.SyncPaths(ctx, indexer, vault, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	schema, err := ontology.LoadSchema(root)
	require.NoError(t, err)
	return schema, store
}

func TestAllocateRootIncludesEmbeddedSibling(t *testing.T) {
	schema, store := indexedIdentifierPool(t, `
type Story implements Section @node(locator: EMBEDDED) { id: ID! @field @identifier(preferred: true, derivable: false, prefix: "ITEM") }
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, prefix: "ITEM")
 stories: StoriesSection @contains(level: H2, heading: "Stories")
}`, map[string]string{"specs/one.md": "---\nid: ITEM-0001\naliases: [ITEM-0001]\n---\n# One\n\n## Stories\n\n### First\nid:: ^ITEM-0002\n"})
	nodes, err := store.OntologyNodesByType(context.Background(), "Story")
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	for _, requested := range []string{"Spec", "Story"} {
		t.Run(requested, func(t *testing.T) {
			result, err := Allocate(context.Background(), schema, store, requested)
			require.NoError(t, err)
			require.Equal(t, "ITEM-0003", result.Next)
			require.Equal(t, 2, result.OwnersScanned)
			require.Equal(t, nodes[0].SourceLocator, result.CurrentMaxOwner)
			require.Contains(t, result.OwnersMatched, nodes[0].SourceLocator+"=ITEM-0002")
			require.Contains(t, result.OwnersMatched, "specs/one.md=ITEM-0001")
			require.Len(t, result.SharedWith, 1)
		})
	}
}

func TestAllocateEmbeddedInventoryUsesEachOwnersDeclaredField(t *testing.T) {
	schema, store := indexedIdentifierPool(t, `
type Story implements Section @node(locator: EMBEDDED) {
 ticketKey: ID! @field @identifier(preferred: true, derivable: false, prefix: "ITEM")
 id: ID! @field
 aliases: [String!] @field
}
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["specs/*.md"]) {
 rootKey: String! @field(source: "root-id") @identifier(preferred: true, prefix: "ITEM")
 stories: StoriesSection @contains(level: H2, heading: "Stories")
}`, map[string]string{"specs/one.md": "---\nROOT-ID: ITEM-0007\nid: ITEM-0999\nticket-key: ITEM-0888\naliases: [ITEM-0007]\n---\n# One\n\n## Stories\n\n### First\nticket-key:: ^ITEM-0009\nid:: ITEM-0777\naliases:: ITEM-0666\n\n### Second\nticket-key:: ^ITEM-0011\nid:: ITEM-0555\n"})
	result, err := Allocate(context.Background(), schema, store, "Spec")
	require.NoError(t, err)
	require.Equal(t, "ITEM-0012", result.Next)
	require.Equal(t, 3, result.OwnersScanned)
	require.Len(t, result.OwnersMatched, 3)
	require.Equal(t, 11, *result.CurrentMax)
	require.Contains(t, result.OwnersMatched, "specs/one.md=ITEM-0007")
	require.Equal(t, []string{"Story"}, result.SharedWith)
}

type observedIdentifierStore struct {
	*semdb.Store
	rawReads, rawRows int
}

func (s *observedIdentifierStore) CurrentNotePropertyValues(ctx context.Context, paths, fields []string, source semdb.NotePropertySource) ([]semdb.NotePropertyValueRow, error) {
	rows, err := s.Store.CurrentNotePropertyValues(ctx, paths, fields, source)
	s.rawReads++
	s.rawRows += len(rows)
	return rows, err
}

func TestAllocateDirectEmbeddedIdentifier(t *testing.T) {
	for _, tc := range []struct {
		strategy, prefix, preferred, alias, want string
		paths                                    []string
	}{
		{"SEQUENTIAL", "ITEM", "ITEM-0001", "ITEM-0666", "ITEM-0002", nil},
		{"DATETIME", "EFF", "EFF-2026-10-04-12-00", "EFF-2026-10-04-12-00-2", "EFF-2026-10-04-12-00-3", []string{"specs/2026-10-04-12-00-new.md"}},
	} {
		t.Run(tc.strategy, func(t *testing.T) {
			schema, store := indexedIdentifierPool(t, fmt.Sprintf(`
type Story implements Section @node(locator: EMBEDDED) { id: ID! @field @identifier(preferred: true, derivable: false, strategy: %s, prefix: "%s") aliases: [String!] @field }
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["specs/*.md"]) { stories: StoriesSection @contains(level: H2, heading: "Stories") }
`, tc.strategy, tc.prefix), map[string]string{
				"specs/one.md": "---\nid: DECOY-0001\n---\n# One\n\n## Stories\n\n### First\nid:: ^" + tc.preferred + "\naliases:: " + tc.alias + "\n",
				"other/one.md": "---\nid: DECOY-0002\naliases: [EFF-2026-10-04-12-00-4]\nsummary: unrelated\n---\n# Unrelated\n",
			})
			observed := &observedIdentifierStore{Store: store}
			result, err := AllocateRequest(context.Background(), schema, observed, Request{Type: "Story", Paths: tc.paths})
			require.NoError(t, err)
			require.Equal(t, tc.want, result.Next)
			require.Equal(t, 1, result.OwnersScanned)
			require.Contains(t, result.OwnersMatched, "specs/one.md#^"+tc.preferred+"="+tc.preferred)
			require.Empty(t, result.SharedWith)
			t.Logf("raw reads=%d returned rows=%d", observed.rawReads, observed.rawRows)
			require.Zero(t, observed.rawReads, "embedded-only pools must not request unrelated frontmatter")
		})
	}
}

func TestAllocateDateTimeEmbeddedAliasesRetainOwnerIdentity(t *testing.T) {
	schema, store := indexedIdentifierPool(t, `
type Story implements Section @node(locator: EMBEDDED) {
 ticketKey: ID! @field @identifier(preferred: true, derivable: false, strategy: DATETIME, prefix: "EFF")
 alias: [String!] @field
 id: ID! @field
}
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Effort @node(paths: ["efforts/*.md"]) {
 id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
 stories: StoriesSection @contains(level: H2, heading: "Stories")
}
type Other @node(paths: ["other/*.md"]) { id: String! @field @identifier(preferred: true, prefix: "OTHER") }`, map[string]string{
		"efforts/one.md": "---\nid: EFF-2026-10-04-08-00\naliases: [EFF-2026-10-04-08-00-2]\nalias: EFF-2026-10-04-08-00-5\n---\n# One\n\n## Stories\n\n### First\nticket-key:: ^EFF-2026-10-04-08-00-3\nalias:: EFF-2026-10-04-08-00-4\nid:: EFF-2026-10-04-08-00-6\n\n### Second\nticket-key:: ^EFF-2026-10-04-07-00\nalias:: EFF-2026-10-04-08-00-4\nalias:: unrelated-name\n",
		"other/decoy.md": "---\nid: OTHER-0001\naliases: [EFF-2026-10-04-08-00-5]\n---\n# Other\n",
	})
	ctx := context.Background()
	nodes, err := store.OntologyNodesByType(ctx, "Story")
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	paths := []string{"efforts/2026-10-04-08-00-z.md", "efforts/2026-10-04-08-00-a.md"}
	result, err := AllocateRequest(ctx, schema, store, Request{Type: "Effort", Paths: paths})
	require.NoError(t, err)
	require.Equal(t, []string{"EFF-2026-10-04-08-00-5", "EFF-2026-10-04-08-00-6"}, result.IDs)
	require.Equal(t, paths, result.Paths)
	require.Equal(t, paths[0], result.Allocations[0].Path)
	require.Equal(t, paths[1], result.Allocations[1].Path)
	require.Equal(t, 3, result.OwnersScanned)
	require.Len(t, result.OwnersMatched, 6)
	for _, node := range nodes {
		require.Contains(t, result.OwnersMatched, node.SourceLocator+"=EFF-2026-10-04-08-00-4")
	}
	require.Empty(t, result.OwnersSkipped)
}

func TestAllocateDateTimeReservesBothEmbeddedAliasFields(t *testing.T) {
	for _, fields := range []string{
		"alias: [String!] @field\naliases: [String!] @field",
		"aliases: [String!] @field\nalias: [String!] @field",
	} {
		t.Run(fields, func(t *testing.T) {
			schema, store := indexedIdentifierPool(t, `
type Story implements Section @node(locator: EMBEDDED) {
 id: ID! @field @identifier(preferred: true, derivable: false, strategy: DATETIME, prefix: "EFF")
 `+fields+`
}
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Effort @node(paths: ["efforts/*.md"]) {
 id: String! @field @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
 stories: StoriesSection @contains(level: H2, heading: "Stories")
}`, map[string]string{"efforts/one.md": "---\nid: EFF-2026-10-04-12-00\naliases: [EFF-2026-10-04-12-00]\n---\n# One\n\n## Stories\n\n### First\nid:: ^EFF-2026-10-04-11-00\nalias:: EFF-2026-10-04-12-00-2\naliases:: EFF-2026-10-04-12-00-3\n"})
			result, err := AllocateRequest(context.Background(), schema, store, Request{Type: "Effort", Paths: []string{"efforts/2026-10-04-12-00-next.md"}})
			require.NoError(t, err)
			require.Equal(t, "EFF-2026-10-04-12-00-4", result.Next)
			require.Equal(t, 2, result.OwnersScanned)
			require.Len(t, result.OwnersMatched, 4)
			require.Contains(t, result.OwnersMatched, "efforts/one.md#^EFF-2026-10-04-11-00=EFF-2026-10-04-12-00-2")
			require.Contains(t, result.OwnersMatched, "efforts/one.md#^EFF-2026-10-04-11-00=EFF-2026-10-04-12-00-3")
		})
	}
}
