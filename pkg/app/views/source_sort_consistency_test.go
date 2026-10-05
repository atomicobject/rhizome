package views

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOntologySourceTextSortAgreesWithResidualSort(t *testing.T) {
	ctx := context.Background()
	service := scalarSortService(t, "String", `# Actions

- [ ] Two #action-item
  rank:: 2
- [ ] Ten #action-item
  rank:: 10
- [ ] Mixed #action-item
  rank:: 1a
- [ ] Date #action-item
  rank:: 2026-05-08
- [ ] Zulu #action-item
  rank:: apple
- [ ] Alpha #action-item
  rank:: APPLE
- [ ] Wrapped #action-item
  rank:: [[banana|Display]]
- [ ] Empty #action-item
`)

	for _, field := range []string{"rank", "inline.rank"} {
		for _, direction := range []string{"asc", "desc"} {
			t.Run(field+"/"+direction, func(t *testing.T) {
				primary := []viewconfig.SortSpec{{Field: field, Direction: direction}}
				pushed, err := service.Execute(ctx, "actions", ExecuteRequest{Sort: primary})
				require.NoError(t, err)
				require.Equal(t, primary, pushed.PushedConstraints.Sort)
				require.Empty(t, pushed.ResidualConstraints.Sort)
				full := append(append([]viewconfig.SortSpec(nil), primary...), viewconfig.SortSpec{Field: "title", Direction: "asc"})
				residual, err := service.Execute(ctx, "actions", ExecuteRequest{Sort: full})
				require.NoError(t, err)
				require.Equal(t, primary, residual.PushedConstraints.Sort)
				require.Equal(t, full, residual.ResidualConstraints.Sort)
				ranks := func(rows []TableRow) []string {
					out := make([]string, 0, len(rows))
					for _, row := range rows {
						value, _ := fieldValue(row, "rank")
						out = append(out, strings.ToLower(valueString(value)))
					}
					return out
				}
				require.Equal(t, ranks(pushed.Rows), ranks(residual.Rows))
				want := []string{"Ten", "Mixed", "Two", "Date", "Alpha", "Zulu", "Wrapped", "Empty"}
				if direction == "desc" {
					want = []string{"Wrapped", "Alpha", "Zulu", "Date", "Two", "Mixed", "Ten", "Empty"}
				}
				require.Equal(t, want, rowTitles(residual.Rows))
			})
		}
	}
}

func scalarSortService(t *testing.T, scalar, content string) *Service {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type ActionItem implements Section
  @node(locator: EMBEDDED)
  @source(shape: CHECKBOX_ITEM, marker: "#action-item") {
  done: Boolean! @field(sourceKind: CHECKBOX)
  rank: `+scalar+` @field
}
`)
	writeSourceFixture(t, root, "notes/actions.md", content)

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)

	return New(ServiceOptions{
		VaultPath:  root,
		VaultDef:   vaultDef,
		NoteReader: &obsidian.Note{},
		Store:      store,
		Schema:     runtime.Schema,
		ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{
			VaultDef:   vaultDef,
			NoteReader: &obsidian.Note{},
			Store:      store,
		},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion,
			ID:         "actions",
			Name:       "Actions",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "ActionItem"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants: viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{
				{Field: "title"},
				{Field: "rank"},
			}}},
		}},
	})
}

func TestOntologySourceDateSortAgreesWithResidualSort(t *testing.T) {
	for _, tc := range []struct {
		scalar string
		values []string
	}{
		{"DateTime", []string{"2026-05-08T09:00:00+02:00", "2026-05-08T08:00:00Z", "2026-05-08T09:00:00.999+02:00"}},
		{"Date", []string{"2026-05-08", "2026-05-09", "2026-05-08"}},
	} {
		t.Run(tc.scalar, func(t *testing.T) {
			content := "# Actions\n\n"
			for i, value := range tc.values {
				title := fmt.Sprintf("Item %d", i)
				if tc.scalar == "Date" {
					title = []string{"Zulu", "Middle", "Alpha"}[i]
				}
				content += fmt.Sprintf("- [ ] %s #action-item\n  rank:: %s\n", title, value)
			}
			content += "- [ ] Empty #action-item\n"
			service := scalarSortService(t, tc.scalar, content)
			for _, field := range []string{"rank", "inline.rank"} {
				for _, direction := range []string{"asc", "desc"} {
					t.Run(field+"/"+direction, func(t *testing.T) {
						primary := []viewconfig.SortSpec{{Field: field, Direction: direction}}
						pushed, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: primary})
						require.NoError(t, err)
						require.Equal(t, primary, pushed.PushedConstraints.Sort)
						require.Empty(t, pushed.ResidualConstraints.Sort)
						full := append(append([]viewconfig.SortSpec(nil), primary...), viewconfig.SortSpec{Field: "title", Direction: "asc"})
						residual, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: full})
						require.NoError(t, err)
						require.Equal(t, primary, residual.PushedConstraints.Sort)
						require.Equal(t, full, residual.ResidualConstraints.Sort)
						values := func(rows []TableRow) []any {
							out := make([]any, 0, len(rows))
							for _, row := range rows {
								value, _ := fieldValue(row, "rank")
								out = append(out, value)
							}
							return out
						}
						require.Equal(t, values(pushed.Rows), values(residual.Rows))
						want := []string{"Item 0", "Item 1", "Item 2", "Empty"}
						if direction == "desc" {
							want = []string{"Item 2", "Item 1", "Item 0", "Empty"}
						}
						if tc.scalar == "DateTime" {
							want = []string{"Item 1", "Item 0", "Item 2", "Empty"}
							if direction == "desc" {
								want = []string{"Item 0", "Item 2", "Item 1", "Empty"}
							}
						} else {
							want = []string{"Alpha", "Zulu", "Middle", "Empty"}
							if direction == "desc" {
								want = []string{"Middle", "Alpha", "Zulu", "Empty"}
							}
						}
						require.Equal(t, want, rowTitles(residual.Rows))
					})
				}
			}
		})
	}
}

func TestOntologySourceNumericSortAgreesWithResidualSort(t *testing.T) {
	for _, tc := range []struct {
		scalar string
		values []string
	}{
		{"Int", []string{"2", "10", "-1", "2", "9007199254740993", "9007199254740992", "invalid", "9223372036854775808"}},
		{"Float", []string{"2.5", "10.5", "-1.5", "2.50", "+Inf", "1e3", "invalid", "NaN"}},
	} {
		t.Run(tc.scalar, func(t *testing.T) {
			titles := []string{"Zulu", "Ten", "Negative", "Alpha", "Large", "Smaller", "Invalid", "Unrepresentable"}
			content := "# Actions\n\n"
			for i, value := range tc.values {
				content += fmt.Sprintf("- [ ] %s #action-item\n  rank:: %s\n", titles[i], value)
			}
			content += "- [ ] Empty #action-item\n"
			service := scalarSortService(t, tc.scalar, content)
			for _, field := range []string{"rank", "inline.rank"} {
				for _, direction := range []string{"asc", "desc"} {
					t.Run(field+"/"+direction, func(t *testing.T) {
						primary := []viewconfig.SortSpec{{Field: field, Direction: direction}}
						pushed, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: primary})
						require.NoError(t, err)
						require.Equal(t, primary, pushed.PushedConstraints.Sort)
						require.Empty(t, pushed.ResidualConstraints.Sort)
						full := append(append([]viewconfig.SortSpec(nil), primary...), viewconfig.SortSpec{Field: "title", Direction: "asc"})
						residual, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: full})
						require.NoError(t, err)
						require.Equal(t, primary, residual.PushedConstraints.Sort)
						require.Equal(t, full, residual.ResidualConstraints.Sort)
						// Compare primary groups, since SQL and title use different tie breaks.
						groups := func(rows []TableRow) []string {
							out := make([]string, len(rows))
							for i, row := range rows {
								switch row.Title {
								case "Zulu", "Alpha":
									out[i] = "tie"
								case "Empty", "Invalid", "Unrepresentable":
									out[i] = "null"
								default:
									out[i] = row.Title
								}
							}
							return out
						}
						require.Equal(t, groups(pushed.Rows), groups(residual.Rows))
						want := []string{"Negative", "Alpha", "Zulu", "Ten", "Smaller", "Large", "Empty", "Invalid", "Unrepresentable"}
						if direction == "desc" {
							want = []string{"Large", "Smaller", "Ten", "Alpha", "Zulu", "Negative", "Empty", "Invalid", "Unrepresentable"}
						}
						require.Equal(t, want, rowTitles(residual.Rows))
					})
				}
			}
		})
	}
}

func TestOntologySourceInvalidDateSortAgreesWithResidualSort(t *testing.T) {
	for _, scalar := range []string{"Date", "DateTime"} {
		t.Run(scalar, func(t *testing.T) {
			valid := "2026-05-08"
			if scalar == "DateTime" {
				valid += "T08:00:00Z"
			}
			service := scalarSortService(t, scalar, "# Actions\n\n"+
				"- [ ] Zulu malformed #action-item\n  rank:: z-invalid\n"+
				"- [ ] Valid #action-item\n  rank:: "+valid+"\n"+
				"- [ ] Alpha malformed #action-item\n  rank:: 0000-99-99\n"+
				"- [ ] Empty #action-item\n")
			for _, field := range []string{"rank", "inline.rank"} {
				for _, direction := range []string{"asc", "desc"} {
					t.Run(field+"/"+direction, func(t *testing.T) {
						primary := []viewconfig.SortSpec{{Field: field, Direction: direction}}
						pushed, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: primary})
						require.NoError(t, err)
						require.Equal(t, primary, pushed.PushedConstraints.Sort)
						require.Empty(t, pushed.ResidualConstraints.Sort)
						require.Equal(t, []string{"Valid", "Zulu malformed", "Alpha malformed", "Empty"}, rowTitles(pushed.Rows))
						for _, secondary := range []string{"asc", "desc"} {
							full := append(append([]viewconfig.SortSpec(nil), primary...), viewconfig.SortSpec{Field: "title", Direction: secondary})
							residual, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: full})
							require.NoError(t, err)
							require.Equal(t, primary, residual.PushedConstraints.Sort)
							require.Equal(t, full, residual.ResidualConstraints.Sort)
							want := []string{"Valid", "Alpha malformed", "Empty", "Zulu malformed"}
							if secondary == "desc" {
								want = []string{"Valid", "Zulu malformed", "Empty", "Alpha malformed"}
							}
							require.Equal(t, want, rowTitles(residual.Rows))
						}
					})
				}
			}
		})
	}
}

func TestOntologySourceRepeatedNumericSortAgreesWithResidualSort(t *testing.T) {
	for _, tc := range []struct{ scalar, low, high, invalid string }{
		{"Int", "9007199254740992", "9007199254740993", "9223372036854775808"},
		{"Float", "900.5", "901.5", "NaN"},
	} {
		t.Run(tc.scalar, func(t *testing.T) {
			content := "# Actions\n\n" +
				"- [ ] Zulu repeated #action-item\n  rank:: 2\n  rank:: 10\n  rank:: invalid\n" +
				"- [ ] Five #action-item\n  rank:: 5\n" +
				"- [ ] Alpha repeated #action-item\n  rank:: 10\n  rank:: 2\n" +
				"- [ ] Low #action-item\n  rank:: " + tc.low + "\n" +
				"- [ ] High #action-item\n  rank:: " + tc.high + "\n  rank:: " + tc.invalid + "\n" +
				"- [ ] Invalid #action-item\n  rank:: invalid\n  rank:: " + tc.invalid + "\n" +
				"- [ ] Empty #action-item\n"
			service := scalarSortService(t, tc.scalar, content)
			for _, field := range []string{"rank", "inline.rank"} {
				for _, direction := range []string{"asc", "desc"} {
					t.Run(field+"/"+direction, func(t *testing.T) {
						primary := []viewconfig.SortSpec{{Field: field, Direction: direction}}
						pushed, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: primary})
						require.NoError(t, err)
						require.Equal(t, primary, pushed.PushedConstraints.Sort)
						require.Empty(t, pushed.ResidualConstraints.Sort)
						groups := func(rows []TableRow) []string {
							out := rowTitles(rows)
							for i, title := range out {
								switch title {
								case "Zulu repeated", "Alpha repeated":
									out[i] = "repeated"
								case "Invalid", "Empty":
									out[i] = "null"
								}
							}
							return out
						}
						wantGroups := []string{"repeated", "repeated", "Five", "Low", "High", "null", "null"}
						if direction == "desc" {
							wantGroups = []string{"High", "Low", "repeated", "repeated", "Five", "null", "null"}
						}
						require.Equal(t, wantGroups, groups(pushed.Rows))
						for _, secondary := range []string{"asc", "desc"} {
							full := append(append([]viewconfig.SortSpec(nil), primary...), viewconfig.SortSpec{Field: "title", Direction: secondary})
							residual, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: full})
							require.NoError(t, err)
							require.Equal(t, primary, residual.PushedConstraints.Sort)
							require.Equal(t, full, residual.ResidualConstraints.Sort)
							require.Equal(t, wantGroups, groups(residual.Rows))
							repeated, nulls := []string{"Alpha repeated", "Zulu repeated"}, []string{"Empty", "Invalid"}
							if secondary == "desc" {
								repeated, nulls = []string{"Zulu repeated", "Alpha repeated"}, []string{"Invalid", "Empty"}
							}
							want := append(append([]string{}, repeated...), "Five", "Low", "High")
							if direction == "desc" {
								want = append(append([]string{"High", "Low"}, repeated...), "Five")
							}
							want = append(want, nulls...)
							require.Equal(t, want, rowTitles(residual.Rows))
						}
					})
				}
			}
		})
	}
}

func TestOntologySourceRepeatedLexicalSortAgreesWithResidualSort(t *testing.T) {
	for _, tc := range []struct{ scalar, low, middle, high, tieLow, tieHigh string }{
		{"String", "[[APPLE|Display]]", "banana", "ZULU", "apple", "[[zulu|Other]]"},
		{"Date", "2026-01-01", "2026-02-01", "2026-03-01", "2026-01-01", "2026-03-01"},
		{"DateTime", "2026-05-08T08:00:00Z", "2026-05-08T09:00:00+02:00", "2026-05-08T10:00:00+03:00", "2026-05-08T08:00:00.999Z", "2026-05-08T10:00:00.999+03:00"},
	} {
		t.Run(tc.scalar, func(t *testing.T) {
			content := "# Actions\n\n" +
				"- [ ] Zulu repeated #action-item\n  rank:: " + tc.low + "\n  rank:: " + tc.high + "\n" +
				"- [ ] Middle #action-item\n  rank:: " + tc.middle + "\n" +
				"- [ ] Alpha repeated #action-item\n  rank:: " + tc.tieHigh + "\n  rank:: " + tc.tieLow + "\n"
			if tc.scalar != "String" {
				content += "  rank:: 0000-99-99\n  rank:: z-invalid\n" +
					"- [ ] Invalid #action-item\n  rank:: 0000-99-99\n  rank:: z-invalid\n"
			}
			content += "- [ ] Empty #action-item\n"
			service := scalarSortService(t, tc.scalar, content)
			for _, field := range []string{"rank", "inline.rank"} {
				for _, direction := range []string{"asc", "desc"} {
					t.Run(field+"/"+direction, func(t *testing.T) {
						primary := []viewconfig.SortSpec{{Field: field, Direction: direction}}
						pushed, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: primary})
						require.NoError(t, err)
						require.Equal(t, primary, pushed.PushedConstraints.Sort)
						require.Empty(t, pushed.ResidualConstraints.Sort)
						groups := func(rows []TableRow) []string {
							out := rowTitles(rows)
							for i, title := range out {
								switch title {
								case "Zulu repeated", "Alpha repeated":
									out[i] = "repeated"
								case "Invalid", "Empty":
									out[i] = "null"
								}
							}
							return out
						}
						wantGroups := []string{"repeated", "repeated", "Middle", "null"}
						if tc.scalar != "String" {
							wantGroups = append(wantGroups, "null")
						}
						require.Equal(t, wantGroups, groups(pushed.Rows))
						for _, secondary := range []string{"asc", "desc"} {
							full := append(append([]viewconfig.SortSpec(nil), primary...), viewconfig.SortSpec{Field: "title", Direction: secondary})
							residual, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: full})
							require.NoError(t, err)
							require.Equal(t, primary, residual.PushedConstraints.Sort)
							require.Equal(t, full, residual.ResidualConstraints.Sort)
							require.Equal(t, groups(pushed.Rows), groups(residual.Rows))
							want := []string{"Alpha repeated", "Zulu repeated", "Middle", "Empty"}
							if tc.scalar != "String" {
								want = append(want, "Invalid")
							}
							if secondary == "desc" {
								want[0], want[1] = want[1], want[0]
								if tc.scalar != "String" {
									want[3], want[4] = want[4], want[3]
								}
							}
							require.Equal(t, want, rowTitles(residual.Rows))
						}
					})
				}
			}
		})
	}
}

func TestOntologySourceRepeatedFrontmatterSortAgreesWithResidualSort(t *testing.T) {
	for _, tc := range []struct{ scalar, low, middle, high string }{
		{"Int", "2", "5", "10"},
		{"Float", "2.5", "5.5", "10.5"},
		{"String", "apple", "mango", "zulu"},
		{"Date", "2026-01-01", "2026-02-01", "2026-03-01"},
		{"DateTime", "2026-01-01T08:00:00Z", "2026-02-01T08:00:00Z", "2026-03-01T08:00:00Z"},
	} {
		t.Run(tc.scalar, func(t *testing.T) {
			service := scalarNoteSortService(t, tc.scalar, tc.low, tc.middle, tc.high)
			for _, direction := range []string{"asc", "desc"} {
				t.Run(direction, func(t *testing.T) {
					primary := []viewconfig.SortSpec{{Field: "rank", Direction: direction}}
					pushed, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: primary})
					require.NoError(t, err)
					require.Equal(t, primary, pushed.PushedConstraints.Sort)
					require.Empty(t, pushed.ResidualConstraints.Sort)
					full := append(append([]viewconfig.SortSpec(nil), primary...), viewconfig.SortSpec{Field: "title", Direction: "asc"})
					residual, err := service.Execute(context.Background(), "actions", ExecuteRequest{Sort: full})
					require.NoError(t, err)
					require.Equal(t, primary, residual.PushedConstraints.Sort)
					require.Equal(t, full, residual.ResidualConstraints.Sort)
					groups := func(rows []TableRow) []string {
						out := rowTitles(rows)
						for i, title := range out {
							if strings.HasSuffix(title, " repeated") {
								out[i] = "repeated"
							}
						}
						return out
					}
					require.Equal(t, []string{"repeated", "repeated", "Middle", "Empty"}, groups(pushed.Rows))
					require.Equal(t, groups(pushed.Rows), groups(residual.Rows))
					require.Equal(t, []string{"Alpha repeated", "Zulu repeated", "Middle", "Empty"}, rowTitles(residual.Rows))
				})
			}
		})
	}
}

func scalarNoteSortService(t *testing.T, scalar, low, middle, high string) *Service {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	writeSourceFixture(t, root, ".rhizome/ontology/schema.graphql", `
type ActionItem @node(paths: ["notes/*.md"]) {
  rank: `+scalar+` @field
}
`)
	for _, name := range []string{"Zulu repeated", "Alpha repeated", "Middle", "Empty"} {
		var values string
		switch name {
		case "Zulu repeated":
			values = fmt.Sprintf("rank: [%q, %q]\n", low, high)
		case "Alpha repeated":
			values = fmt.Sprintf("rank: [%q, %q]\n", high, low)
		case "Middle":
			values = fmt.Sprintf("rank: %q\n", middle)
		}
		writeSourceFixture(t, root, "notes/"+strings.ToLower(strings.ReplaceAll(name, " ", "-"))+".md", "---\n"+values+"---\n# "+name+"\n")
	}
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "view-fixture", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	execSchema, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)
	return New(ServiceOptions{
		VaultPath: root, VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: store,
		Schema: runtime.Schema, ExecSchema: execSchema,
		QueryDeps: ontologyquery.Deps{VaultDef: vaultDef, NoteReader: &obsidian.Note{}, Store: store},
		Views: []viewconfig.ViewDefinition{{
			APIVersion: viewconfig.APIVersion, ID: "actions", Name: "Actions",
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: "ActionItem"},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants:   viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: []viewconfig.ViewColumn{{Field: "title"}, {Field: "rank"}}}},
		}},
	})
}
