package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestStructuralSnapshotBoundaryUsesSelectedProviderCapabilities(t *testing.T) {
	for _, fixture := range []struct {
		name, providerID, rowID, path, extension string
		structural, wantRoot                     bool
		wantReads                                int
	}{
		{"Markdown provider without structural syntax", "markdown", "markdown", "source.md", ".md", false, true, 1},
		{"foreign structural provider remains a root", "future", "future", "source.future", ".future", true, true, 1},
		{"foreign provider owns legacy Markdown path", "future", "markdown", "source.md", ".md", false, false, 0},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			provider := structuralBoundaryProvider{id: noteformat.FormatID(fixture.providerID), extension: fixture.extension, structural: fixture.structural}
			registry, err := noteformat.NewRegistry(provider)
			require.NoError(t, err)
			formats, err := noteformat.NewRuntime(registry, provider)
			require.NoError(t, err)
			store := &selectorSnapshotStore{boundedHydrationStore: &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{
				fixture.path: {Path: fixture.path, FormatID: fixture.rowID, Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
			}}}
			reader := &rootLinkFactsReader{selectorSnapshotReader: selectorSnapshotReader{content: "# Authored heading\n\n## Foreign syntax must stay opaque\n"}}
			scope := NewService(obsidian.VaultDefinition{}, reader, store, &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{}}).WithNoteFormats(formats).NewScope(ScopeOptions{})
			projection, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: fixture.path, Kind: ontology.NodeKindNote})
			require.NoError(t, err)
			if fixture.wantRoot {
				require.NotNil(t, projection)
				require.NotNil(t, projection.RootSnapshot)
				require.Equal(t, fixture.providerID, string(projection.RootSnapshot.Format))
				require.Equal(t, "Provider title", projection.RootSnapshot.Title)
				require.Nil(t, projection.Snapshot)
				require.Nil(t, projection.RootSnapshot.MarkdownStructure)
			} else {
				require.Nil(t, projection)
			}
			require.Equal(t, fixture.wantReads, reader.reads)
		})
	}
}

type structuralBoundaryProvider struct {
	id         noteformat.FormatID
	extension  string
	structural bool
}

func (p structuralBoundaryProvider) Descriptor() noteformat.Descriptor {
	capabilities := []noteformat.Capability{noteformat.CapabilitySourceReading, noteformat.CapabilityRootMetadataReading}
	if p.structural {
		capabilities = append(capabilities, noteformat.CapabilityStructuralContentMutation)
	}
	return noteformat.Descriptor{ID: p.id, Extensions: []string{p.extension}, ProviderVersion: "test-v1", ProjectionVersion: "test-v1", OwnershipPolicy: noteformat.OwnershipDefault, Capabilities: noteformat.MustCapabilities(capabilities...)}
}
func (p structuralBoundaryProvider) Project(noteformat.AuthoredSource) (noteformat.Projection, error) {
	d := p.Descriptor()
	return noteformat.NewProjectionWithFacts(d.ProviderVersion, d.ProjectionVersion, noteformat.ProjectionStatusCurrent, nil, d.Capabilities, noteformat.ProjectionFacts{Title: &noteformat.TitleFact{Value: "Provider title"}})
}
