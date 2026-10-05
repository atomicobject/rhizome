package views

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const relationReadsSchema = `
type Person @node(paths: ["people/**/*.md"]) {
  name: String @field(source: "name")
}

type Opportunity @node(paths: ["opportunities/**/*.md"]) {
  owner: Person @link(source: "owner")
  ideas: [Idea!] @reverse(field: "opportunities")
}

type Idea @node(paths: ["ideas/**/*.md"]) {
  status: String @field(source: "status")
  opportunities: [Opportunity!] @link(source: "opportunities")
  category: Opportunity @link(source: "category")
  owner: Person @link(source: "owner")
}
`

// countingStore counts the reads that used to repeat per row: catalog nodes
// by source locator (one per Resolve) and indexed field values.
type countingStore struct {
	*semdb.Store
	locators    atomic.Int64
	fieldValues atomic.Int64
}

func (s *countingStore) OntologyNodesBySourceLocators(ctx context.Context, locators []string) (map[string]codeanchor.IntelOntologyNode, error) {
	s.locators.Add(1)
	return s.Store.OntologyNodesBySourceLocators(ctx, locators)
}

func (s *countingStore) OntologyNodeFieldValuesByNodeIDs(ctx context.Context, nodeIDs, fields []string) ([]codeanchor.IntelOntologyNodeFieldValue, error) {
	s.fieldValues.Add(1)
	return s.Store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, fields)
}

// countingReader counts vault file listings, which walk the vault directory.
type countingReader struct {
	obsidian.Note
	listings atomic.Int64
}

func (r *countingReader) GetNotesList(vault obsidian.VaultDefinition) ([]string, error) {
	r.listings.Add(1)
	return r.Note.GetNotesList(vault)
}

// relationReadsFixture indexes ideas with three link fields each, the
// opportunities and people they link to, and unrelated filler notes.
func relationReadsFixture(tb testing.TB, ideas, filler int) (string, obsidian.VaultDefinition, *semdb.Store, *ontology.Schema) {
	tb.Helper()
	ctx := context.Background()
	root := tb.TempDir()
	writeSourceFixture(tb, root, ".rhizome/ontology/schema.graphql", relationReadsSchema)
	for i := range 10 {
		writeSourceFixture(tb, root, fmt.Sprintf("people/person-%02d.md", i), fmt.Sprintf("---\nname: Person %02d\n---\n# Person %02d\n", i, i))
	}
	for i := range 20 {
		writeSourceFixture(tb, root, fmt.Sprintf("opportunities/opp-%02d.md", i), fmt.Sprintf("---\nowner: \"[[person-%02d]]\"\n---\n# Opportunity %02d\n", i%10, i))
	}
	for i := range ideas {
		writeSourceFixture(tb, root, fmt.Sprintf("ideas/idea-%03d.md", i), fmt.Sprintf(
			"---\nstatus: open\nopportunities:\n  - \"[[opp-%02d]]\"\n  - \"[[opportunities/opp-%02d|Alias]]\"\ncategory: \"[[opp-%02d]]\"\nowner: \"[[people/person-%02d]]\"\n---\n# Idea %03d\n",
			i%20, (i+1)%20, (i+2)%20, i%10, i))
	}
	for i := range filler {
		writeSourceFixture(tb, root, fmt.Sprintf("notes/note-%04d.md", i), fmt.Sprintf("# Note %d\n\nSee [[person-%02d]].\n", i, i%10))
	}
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = store.Close() })
	vaultDef := obsidian.VaultDefinition{Name: "relation-reads", Path: root, Links: obsidian.LinkTypeBoth}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(ctx, testNoteMetadataIndexer(tb), vaultDef, &obsidian.Note{}, store)
	require.NoError(tb, err)
	return root, vaultDef, store, runtime.Schema
}

func relationReadsService(tb testing.TB, root string, vaultDef obsidian.VaultDefinition, store *countingStore, reader *countingReader, schema *ontology.Schema, overlay *ontologyquery.ReadOverlay) *Service {
	tb.Helper()
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	require.NoError(tb, err)
	table := func(fields ...string) viewconfig.VariantSet {
		columns := make([]viewconfig.ViewColumn, 0, len(fields))
		for _, field := range fields {
			columns = append(columns, viewconfig.ViewColumn{Field: field})
		}
		return viewconfig.VariantSet{Table: &viewconfig.TableVariant{Columns: columns}}
	}
	view := func(id, typeName string, variants viewconfig.VariantSet) viewconfig.ViewDefinition {
		return viewconfig.ViewDefinition{
			APIVersion: viewconfig.APIVersion, ID: id, Name: id,
			SourceSpec: viewconfig.SourceSpec{Kind: viewconfig.SourceKindOntologyType, Type: typeName},
			Mount:      viewconfig.MountSpec{Kind: viewconfig.MountKindStandalone},
			Variants:   variants,
		}
	}
	return New(ServiceOptions{
		VaultPath: root, VaultDef: vaultDef, NoteReader: reader, Store: store, Schema: schema, ExecSchema: execSchema,
		QueryDeps:   ontologyquery.Deps{VaultDef: vaultDef, NoteReader: reader, Store: store, ReadOverlay: overlay},
		ReadOverlay: overlay,
		Views: []viewconfig.ViewDefinition{
			view("ideas", "Idea", table("title", "status", "opportunities", "category", "owner")),
			view("opportunities", "Opportunity", table("title", "owner", "ideas")),
		},
	})
}

// Committed note rows read their link targets from the indexed field values
// in one batched read, however many rows link, and never build the
// vault-wide inventory link resolution needs.
func TestNoteLinkTargetsReadIndexedFieldValuesInOneBatch(t *testing.T) {
	ctx := context.Background()
	root, vaultDef, base, schema := relationReadsFixture(t, 200, 0)
	store, reader := &countingStore{Store: base}, &countingReader{}
	service := relationReadsService(t, root, vaultDef, store, reader, schema, nil)

	response, err := service.Execute(ctx, "ideas", ExecuteRequest{Page: PageRequest{First: 500}, PageSet: true})
	require.NoError(t, err)
	require.Len(t, response.Rows, 200)
	for _, row := range response.Rows {
		require.Len(t, row.RelationValues["opportunities"], 2, row.Path)
		for _, field := range []string{"opportunities", "category", "owner"} {
			for _, value := range row.RelationValues[field] {
				require.NotNil(t, value.Ref, "%s %s %s", row.Path, field, value.Value)
				require.NotEmpty(t, value.Title, "%s %s %s", row.Path, field, value.Value)
			}
		}
	}
	idea := response.Rows[0]
	require.Equal(t, "ideas/idea-000.md", idea.Path)
	require.Equal(t, "opportunities/opp-01.md", idea.RelationValues["opportunities"][1].Ref.NotePath)
	require.Equal(t, "Opportunity 01", idea.RelationValues["opportunities"][1].Title)
	require.Equal(t, "people/person-00.md", idea.RelationValues["owner"][0].Ref.NotePath)

	require.Equal(t, int64(1), store.locators.Load(), "one batched catalog read finds every row's node")
	require.Equal(t, int64(1), store.fieldValues.Load(), "one batched read of every row's indexed fields")
	require.Zero(t, reader.listings.Load(), "committed link targets never list the vault to resolve links")

	opportunities, err := service.Execute(ctx, "opportunities", ExecuteRequest{})
	require.NoError(t, err)
	require.Len(t, opportunities.Rows, 20)
	require.Equal(t, "opportunities/opp-00.md", opportunities.Rows[0].Path)
	require.EqualValues(t, 20, opportunities.Rows[0].Fields["ideas"])
	require.Zero(t, reader.listings.Load(), "note relation counts never list the vault to canonicalize note refs")
}

// A staged link edit resolves its new target, and an untouched row keeps its
// indexed one.
func TestStagedNoteLinkEditResolvesNewTarget(t *testing.T) {
	ctx := context.Background()
	root, vaultDef, base, schema := relationReadsFixture(t, 3, 0)
	overlay := &ontologyquery.ReadOverlay{
		SourceFormat: "markdown",
		UpdatedContentByPath: map[string]string{
			"ideas/idea-000.md": "---\nstatus: open\nopportunities:\n  - \"[[opp-07]]\"\ncategory: \"[[opportunities/opp-08]]\"\nowner: \"[[../people/person-09]]\"\n---\n# Idea 000\n",
		},
		TouchedPaths: []string{"ideas/idea-000.md"},
	}
	index, err := noderead.BuildReadOverlayIndex(ctx, schema, overlay)
	require.NoError(t, err)
	overlay.Index = index
	service := relationReadsService(t, root, vaultDef, &countingStore{Store: base}, &countingReader{}, schema, overlay)

	response, err := service.Execute(ctx, "ideas", ExecuteRequest{})
	require.NoError(t, err)
	require.Len(t, response.Rows, 3)
	byPath := map[string]TableRow{}
	for _, row := range response.Rows {
		byPath[row.Path] = row
	}
	staged := byPath["ideas/idea-000.md"]
	requireRelationTarget(t, staged, "opportunities", "opportunities/opp-07.md", "Opportunity 07")
	requireRelationTarget(t, staged, "category", "opportunities/opp-08.md", "Opportunity 08")
	requireRelationTarget(t, staged, "owner", "people/person-09.md", "Person 09")
	requireRelationTarget(t, byPath["ideas/idea-001.md"], "category", "opportunities/opp-03.md", "Opportunity 03")
}

func requireRelationTarget(t *testing.T, row TableRow, field, path, title string) {
	t.Helper()
	values := row.RelationValues[field]
	require.Len(t, values, 1, "%s %s: %#v", row.Path, field, row.RelationValues)
	require.NotNil(t, values[0].Ref, "%s %s", row.Path, field)
	require.Equal(t, path, values[0].Ref.NotePath)
	require.Equal(t, title, values[0].Title)
}

// BenchmarkExecuteNoteLinkFields executes note-type tables whose rows carry
// several link fields, and a reverse-link count column, in a vault with
// unrelated notes.
func BenchmarkExecuteNoteLinkFields(b *testing.B) {
	ctx := context.Background()
	root, vaultDef, base, schema := relationReadsFixture(b, 300, 2000)
	service := relationReadsService(b, root, vaultDef, &countingStore{Store: base}, &countingReader{}, schema, nil)
	for _, viewID := range []string{"ideas", "opportunities"} {
		b.Run(viewID, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := service.Execute(ctx, viewID, ExecuteRequest{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
