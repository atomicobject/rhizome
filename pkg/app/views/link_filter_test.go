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

func TestRelationContainsReportsUnsupportedOperatorEvenWithNoRows(t *testing.T) {
	_, err := applySearchAndFilters(nil, "", []viewconfig.FilterSpec{{Field: "opportunities", Op: "contains", Value: "Rhizome"}}, []FieldCapability{{Key: "opportunities", ValueKind: "relation", FilterOps: []string{"eq", "in", "exists"}}})
	require.ErrorContains(t, err, `link field "opportunities" supports eq, in, exists, or missing`)

	rows, err := applySearchAndFilters([]TableRow{{Fields: map[string]any{"summary": "Rhizome public use"}}}, "", []viewconfig.FilterSpec{{Field: "summary", Op: "contains", Value: "public"}}, []FieldCapability{{Key: "summary", ValueKind: "string"}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestInterfaceLinkFiltersPreserveResolutionErrors(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type Person @node(paths: ["people/*.md"]) { aliases: [String!] @field }
interface Assigned { assignee: Person @link }
type Task implements Assigned @node(paths: ["tasks/*.md"]) { assignee: Person @link }
`)
	writeSourceFixture(t, root, "people/alice.md", "---\naliases: [Shared, Alice Primary]\n---\n# Alice\n")
	writeSourceFixture(t, root, "people/bob.md", "---\naliases: [Shared]\n---\n# Bob\n")
	writeSourceFixture(t, root, "tasks/one.md", "---\nassignee: '[[people/alice]]'\n---\n# Task\n")
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "link-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	service := New(ServiceOptions{
		VaultPath: root, VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: store, Schema: runtime.Schema,
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion, ID: "assigned", Name: "Assigned",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyInterface, Interface: "Assigned"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}, {Field: "assignee"}}}},
		}},
	})
	for _, op := range []string{"eq", "in"} {
		for _, residualSort := range []bool{false, true} {
			for _, tc := range []struct{ target, wantError string }{
				{"Alice Primary", ""}, {"Missing", "did not resolve"}, {"Shared", "multiple Person targets"},
			} {
				t.Run(op+"/"+tc.target+map[bool]string{false: "/collapsed", true: "/per-type"}[residualSort], func(t *testing.T) {
					request := ExecuteRequest{Filters: []viewconfig.FilterSpec{{Field: "assignee", Op: op, Value: tc.target, Values: []string{tc.target}}}}
					if residualSort {
						request.Sort = []viewconfig.SortSpec{{Field: "unindexed", Direction: "asc"}}
					}
					response, err := service.Execute(ctx, "assigned", request)
					if tc.wantError != "" {
						require.ErrorIs(t, err, ErrInvalidRequest)
						require.ErrorContains(t, err, tc.wantError)
					} else {
						require.NoError(t, err)
						require.Equal(t, []string{"tasks/one.md"}, rowPaths(response.Rows))
					}
				})
			}
		}
	}
}
