package noderead

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestLiveMarkdownSnapshotRetainsProviderSelectorFacts(t *testing.T) {
	for _, fixture := range []struct {
		name string
		tags []noteformat.TagFact
		want []string
	}{
		{"provider-only tags", []noteformat.TagFact{{Value: "Provider-Only"}}, []string{"provider-only"}},
		{"authoritatively empty tags", nil, []string{}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".rhizome", "ontology")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(`
				type Subject @node(matches: ["find:ProviderUnique"]) {
				  detail: Detail @contains(level: H2, heading: "Detail")
				}
				type Detail implements Section { status: String @field }
			`), 0o644))
			schema, err := ontoschema.LoadSchema(root)
			require.NoError(t, err)
			provider := selectorSnapshotProvider{tags: fixture.tags}
			registry, err := noteformat.NewRegistry(provider)
			require.NoError(t, err)
			formats, err := noteformat.NewRuntime(registry, provider)
			require.NoError(t, err)
			store := &selectorSnapshotStore{boundedHydrationStore: &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{
				"source.md": {Path: "source.md", Title: "Stale catalog title", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
			}}}
			reader := selectorSnapshotReader{content: "# Different authored title\n\n## Detail\n^detail\n\n#source-only\n"}
			scope := NewService(obsidian.VaultDefinition{}, reader, store, schema).WithNoteFormats(formats).NewScope(ScopeOptions{})
			projection, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: "source.md", Kind: ontology.NodeKindNote})
			require.NoError(t, err)
			require.NotNil(t, projection)
			require.Equal(t, "Subject", projection.ResolvedType)
			facts := projection.Snapshot.SelectorFacts()
			facts.Title = "Caller mutation"
			if len(facts.Tags) > 0 {
				facts.Tags[0] = "caller-mutation"
			}
			nodes, err := ontology.ProjectDocumentNodesFromSnapshot(projection.Snapshot, schema)
			require.NoError(t, err)
			var detail ontology.NodeRef
			for _, node := range nodes {
				if node.ResolvedType == "Detail" {
					detail = node.Ref
				}
			}
			require.False(t, detail.IsZero())
			records, err := scope.Hydrate(context.Background(), []ontology.NodeRef{detail}, HydrateOptions{Profile: HydrateContent})
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Equal(t, fixture.want, records[0].Tags)
		})
	}
}

type selectorSnapshotProvider struct{ tags []noteformat.TagFact }

func (selectorSnapshotProvider) Descriptor() noteformat.Descriptor {
	return markdown.New().Descriptor()
}
func (p selectorSnapshotProvider) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	projection, err := markdown.New().Project(source)
	projection.Facts.Title = &noteformat.TitleFact{Value: "ProviderUnique"}
	projection.Facts.Tags = p.tags
	return projection, err
}

type selectorSnapshotReader struct{ content string }

func (r selectorSnapshotReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return r.content, nil
}
func (selectorSnapshotReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return []string{"source.md"}, nil
}
func (selectorSnapshotReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, nil
}

func (selectorSnapshotReader) Title(string) (string, bool) { return "Different authored title", true }

type selectorSnapshotStore struct{ *boundedHydrationStore }

func (*selectorSnapshotStore) CurrentNoteAliases(context.Context) (map[string][]string, error) {
	return map[string][]string{}, nil
}
