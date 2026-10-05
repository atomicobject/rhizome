package views

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestReverseEmbeddedCountsPreserveDistinctNodesBeforeViewFiltering(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type Person @node(paths: ["people/*.md"]) {
  tasks: [ZTask!] @reverse(field: "assignee")
  reviews: [ZTask!] @reverse(field: "reviewer")
}
type ZTask implements Section @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#task") {
  assignee: Person @link
  reviewer: Person @link
}
`)
	writeSourceFixture(t, root, "people/alice.md", "# Alice\n")
	writeSourceFixture(t, root, "people/bob.md", "# Bob\n")
	writeSourceFixture(t, root, "notes/tasks.md", `# Tasks

- [ ] First #task
  assignee:: [[people/alice]]
  reviewer:: [[people/bob]]
- [ ] Second #task
  assignee:: [[people/alice]]
- [ ] Mention [[people/alice]] in prose #task
`)
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "reverse-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	definition := viewconfig.ViewDefinition{
		APIVersion: viewconfig.APIVersion,
		ID:         "people",
		Name:       "People",
		SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "Person"},
		Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
		Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}, {Field: "tasks"}, {Field: "reviews"}}}},
	}
	service := New(ServiceOptions{
		VaultPath: root, VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: store, Schema: runtime.Schema,
		Views: []viewconfig.ViewDefinition{definition},
	})
	response, err := service.Execute(ctx, "people", ExecuteRequest{
		Filters: []viewconfig.FilterSpec{{Field: "tasks", Op: "gte", Value: "2"}},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"people/alice.md"}, rowPaths(response.Rows))
	require.Equal(t, 2, response.Rows[0].Fields["tasks"])
	require.Equal(t, 0, response.Rows[0].Fields["reviews"])
}
