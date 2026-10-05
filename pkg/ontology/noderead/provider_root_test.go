package noderead

import (
	"context"
	"fmt"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type providerRootReader struct{ content string }

func (r providerRootReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return r.content, nil
}
func (providerRootReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return []string{"prototype.html"}, nil
}
func (providerRootReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Unix(1, 0), nil
}
func (providerRootReader) Title(string) (string, bool) { return "Prototype", true }

func TestProviderRootProjectionDisclosesFormatAndAvoidsMarkdownStructure(t *testing.T) {
	content := `<!doctype html><h1>Body</h1>`
	provider := providerRootProjector{}
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	store := &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{
		"prototype.html": {Path: "prototype.html", Title: "Prototype", FormatID: "html", Mtime: 1, Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
	}}
	service := NewService(obsidian.VaultDefinition{}, providerRootReader{content: content}, store, &ontology.Schema{Types: map[string]*ontology.NoteType{}}).WithNoteFormats(formats)
	scope := service.NewScope(context.Background(), ScopeOptions{})

	records, err := scope.Hydrate(context.Background(), []ontology.NodeRef{{NotePath: "prototype.html", Kind: ontology.NodeKindNote}}, HydrateOptions{Profile: HydrateSummary})
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, noteformat.FormatID("html"), records[0].Format)
	require.Contains(t, records[0].Capabilities, noteformat.CapabilityActiveContentViewing)

	projection, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: "prototype.html", Kind: ontology.NodeKindNote})
	require.NoError(t, err)
	require.NotNil(t, projection)
	require.Nil(t, projection.Snapshot)
	require.NotNil(t, projection.RootSnapshot)
	require.Equal(t, "html", string(projection.RootSnapshot.Format))
}

type providerRootProjector struct{}

func (providerRootProjector) Descriptor() noteformat.Descriptor {
	return noteformat.Descriptor{
		ID: "html", Extensions: []string{".html"},
		ProviderVersion: "test-html-v1", ProjectionVersion: "test-root-v1",
		OwnershipPolicy: noteformat.OwnershipExplicitInclude,
		Capabilities: noteformat.MustCapabilities(
			noteformat.CapabilitySourceReading,
			noteformat.CapabilityRootMetadataReading,
			noteformat.CapabilityActiveContentViewing,
		),
	}
}

func (p providerRootProjector) Project(noteformat.AuthoredSource) (noteformat.Projection, error) {
	value, err := noteformat.NewMetadataValue("active")
	if err != nil {
		return noteformat.Projection{}, err
	}
	descriptor := p.Descriptor()
	return noteformat.NewProjectionWithFacts(descriptor.ProviderVersion, descriptor.ProjectionVersion, noteformat.ProjectionStatusCurrent, nil, descriptor.Capabilities, noteformat.ProjectionFacts{
		Title:        &noteformat.TitleFact{Value: "Prototype"},
		RootMetadata: []noteformat.RootMetadataFact{{Key: "status", Value: value}},
	})
}

func TestProviderRootProjectionsBatchHostReadsAndReuseSources(t *testing.T) {
	provider := providerRootProjector{}
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	store := &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{}}
	refs := make([]ontology.NodeRef, 0, 20)
	for i := range 20 {
		path := fmt.Sprintf("source-%d.html", i)
		store.rows[path] = semdb.NoteMetadataRow{Path: path, FormatID: "html", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}}
		refs = append(refs, ontology.NodeRef{NotePath: path, Kind: ontology.NodeKindNote})
	}
	reader := &boundedHydrationReader{}
	scope := NewService(obsidian.VaultDefinition{}, reader, store, &ontology.Schema{Types: map[string]*ontology.NoteType{}}).WithNoteFormats(formats).NewScope(context.Background(), ScopeOptions{})
	refs = append(refs, refs[0], ontology.NodeRef{NotePath: "missing.html", Kind: ontology.NodeKindNote}, ontology.NodeRef{})
	for range 2 {
		projections, err := scope.Projections(context.Background(), refs)
		require.NoError(t, err)
		require.Len(t, projections, len(refs))
		for i := range 20 {
			require.NotNil(t, projections[i])
			require.Equal(t, refs[i].NotePath, projections[i].Ref.NotePath)
		}
		require.Same(t, projections[0], projections[20])
		require.Nil(t, projections[21])
		require.Nil(t, projections[22])
		require.Equal(t, 1, store.metadataCalls)
		require.Equal(t, 1, store.typeCalls)
		require.Equal(t, 1, store.assessmentCalls)
		require.Equal(t, 21, store.metadataPaths)
		require.Equal(t, 20, reader.contents)
		projection, err := scope.Projection(context.Background(), refs[0])
		require.NoError(t, err)
		require.Same(t, projections[0], projection)
	}
}
