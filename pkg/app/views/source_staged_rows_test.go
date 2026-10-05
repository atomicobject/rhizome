package views

import (
	"context"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const stagedRowsSchema = `
type Person @node(paths: ["people/**/*.md"]) {
  name: String @field(source: "name")
}

type Opportunity @node(paths: ["opportunities/**/*.md"]) {
  evidenceAsOf: Date @field(source: "evidence-as-of")
  owner: Person @link(source: "owner")
}

type Meeting @node(paths: ["meetings/**/*.md"]) {
  actionItems: [ActionItem!] @contains(shape: CHECKBOX_ITEM, marker: "#action-item")
}

type ActionItem implements Section @node(locator: EMBEDDED) {
  done: Boolean! @field(sourceKind: CHECKBOX)
  assignedTo: Person @link
}
`

func newStagedRowsFixture(t *testing.T) (string, obsidian.VaultDefinition, *semdb.Store, *ontology.Schema) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", stagedRowsSchema)
	writeSourceFixture(t, root, "people/alice.md", "---\nname: Alice\n---\n# Alice\n")
	writeSourceFixture(t, root, "opportunities/deal.md", "---\nevidence-as-of: 2026-09-22\nowner: \"[[people/alice]]\"\n---\n# Deal\n")
	writeSourceFixture(t, root, "meetings/demo.md", "# Demo\n\n- [ ] Follow up assigned-to:: [[people/alice]] #action-item\n")
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "staged-rows", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	return root, vaultDef, store, runtime.Schema
}

func stagedRowsService(t *testing.T, root string, vaultDef obsidian.VaultDefinition, store *semdb.Store, schema *ontology.Schema, overlay *ontologyquery.ReadOverlay) *Service {
	t.Helper()
	if overlay != nil {
		index, err := noderead.BuildReadOverlayIndex(context.Background(), schema, overlay)
		require.NoError(t, err)
		overlay.Index = index
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(t, err)
	view := func(id, typeName string) viewconfig.ViewDefinition {
		return viewconfig.ViewDefinition{
			APIVersion: viewconfig.APIVersion,
			ID:         id,
			Name:       id,
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: typeName},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}}}},
		}
	}
	return New(ServiceOptions{
		VaultPath:   root,
		VaultDef:    vaultDef,
		NoteReader:  &obsidian.Note{},
		Store:       store,
		Schema:      schema,
		ExecSchema:  execSchema,
		QueryDeps:   ontologyquery.Deps{VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: store, ReadOverlay: overlay},
		ReadOverlay: overlay,
		Views:       []viewconfig.ViewDefinition{view("opportunities", "Opportunity"), view("actions", "ActionItem")},
	})
}

func TestStagedRowsMatchCommittedDatesAndRelationValues(t *testing.T) {
	ctx := context.Background()
	root, vaultDef, store, schema := newStagedRowsFixture(t)
	staged := &ontologyquery.ReadOverlay{
		SourceFormat: "markdown",
		UpdatedContentByPath: map[string]string{
			"opportunities/deal.md": "---\nevidence-as-of: 2026-09-23\nowner: \"[[people/alice]]\"\n---\n# Deal\n",
			"meetings/demo.md":      "# Demo\n\n- [x] Follow up assigned-to:: [[people/alice]] #action-item\n",
		},
		TouchedPaths: []string{"opportunities/deal.md", "meetings/demo.md"},
	}
	for name, overlay := range map[string]*ontologyquery.ReadOverlay{"committed": nil, "staged": staged} {
		t.Run(name, func(t *testing.T) {
			service := stagedRowsService(t, root, vaultDef, store, schema, overlay)

			opportunities, err := service.Execute(ctx, "opportunities", ExecuteRequest{})
			require.NoError(t, err)
			require.Len(t, opportunities.Rows, 1)
			row := opportunities.Rows[0]
			wantDate := "2026-09-22"
			if overlay != nil {
				wantDate = "2026-09-23"
			}
			require.Equal(t, wantDate, row.Fields["evidenceAsOf"])
			require.Equal(t, wantDate, row.Fields["frontmatter"].(map[string]any)["evidence-as-of"])
			requireTitledRelation(t, row, "owner", "Alice")

			filtered, err := service.Execute(ctx, "opportunities", ExecuteRequest{
				Filters: []viewconfig.FilterSpec{{Field: "evidenceAsOf", Op: "eq", Value: wantDate}},
			})
			require.NoError(t, err)
			require.Len(t, filtered.Rows, 1, "a date filter matches the row's own date")

			actions, err := service.Execute(ctx, "actions", ExecuteRequest{})
			require.NoError(t, err)
			require.Len(t, actions.Rows, 1)
			requireTitledRelation(t, actions.Rows[0], "assignedTo", "Alice")
		})
	}
}

func requireTitledRelation(t *testing.T, row TableRow, field, title string) {
	t.Helper()
	values := row.RelationValues[field]
	require.Len(t, values, 1, "relation values for %s: %#v", field, row.RelationValues)
	require.NotNil(t, values[0].Ref)
	require.Equal(t, "people/alice.md", values[0].Ref.NotePath)
	require.Equal(t, title, values[0].Title)
}
