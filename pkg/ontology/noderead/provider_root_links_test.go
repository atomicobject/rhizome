package noderead

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestMarkdownRootProjectionRetainsAuthoritativeProviderLinks(t *testing.T) {
	for _, order := range []string{"root first", "section first"} {
		for _, links := range []string{"provider replacement", "authoritative empty"} {
			t.Run(order+"/"+links, func(t *testing.T) {
				content := "# Source\n[[target#^raw]]\n\n## Detail\n^detail\n"
				provider := &rootLinkFactsProvider{suppress: links == "authoritative empty"}
				registry, err := noteformat.NewRegistry(provider)
				require.NoError(t, err)
				formats, err := noteformat.NewRuntime(registry, provider)
				require.NoError(t, err)
				reader := &rootLinkFactsReader{selectorSnapshotReader: selectorSnapshotReader{content: content}}
				store := &selectorSnapshotStore{boundedHydrationStore: &boundedHydrationStore{rows: map[string]semdb.NoteMetadataRow{
					"source.md": {Path: "source.md", FormatID: "markdown", Projection: semdb.NoteProjectionState{Status: semdb.NoteProjectionStatusCurrent}},
				}}}
				scope := NewService(obsidian.VaultDefinition{}, reader, store, &ontoschema.Schema{Types: map[string]*ontoschema.NoteType{}}).WithNoteFormats(formats).NewScope(ScopeOptions{})
				if order == "section first" {
					section, err := scope.Projection(context.Background(), ontology.NodeRef{NotePath: "source.md", Kind: ontology.NodeKindSection, Fragment: "^detail"})
					require.NoError(t, err)
					require.NotNil(t, section)
				}
				ref := ontology.NodeRef{NotePath: "source.md", Kind: ontology.NodeKindNote}
				root, err := scope.Projection(context.Background(), ref)
				require.NoError(t, err)
				require.NotNil(t, root)
				require.NotNil(t, root.RootSnapshot)
				require.NotNil(t, root.Snapshot)
				require.Same(t, root.Snapshot, root.RootSnapshot.MarkdownStructure)
				require.Equal(t, content, string(root.RootSnapshot.RawSource))
				facts := root.RootSnapshot.Projection.Facts.Links
				if provider.suppress {
					require.Empty(t, facts)
				} else {
					require.Len(t, facts, 1)
					require.Equal(t, "target#^provider", facts[0].ResolverInput)
					require.Equal(t, "^provider", facts[0].Fragment)
				}
				again, err := scope.Projection(context.Background(), ref)
				require.NoError(t, err)
				require.Same(t, root, again)
				require.Equal(t, 1, reader.reads)
				require.Equal(t, 1, provider.calls)
			})
		}
	}
}

type rootLinkFactsProvider struct {
	suppress bool
	calls    int
}

func (*rootLinkFactsProvider) Descriptor() noteformat.Descriptor { return markdown.New().Descriptor() }
func (p *rootLinkFactsProvider) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	p.calls++
	projection, err := markdown.New().Project(source)
	if p.suppress {
		projection.Facts.Links = nil
	} else if len(projection.Facts.Links) > 0 {
		link := &projection.Facts.Links[0]
		link.Fragment, link.Target, link.ResolverInput = "^provider", "target#^provider", "target#^provider"
	}
	return projection, err
}

type rootLinkFactsReader struct {
	selectorSnapshotReader
	reads int
}

func (r *rootLinkFactsReader) GetContents(vault obsidian.VaultDefinition, path string) (string, error) {
	r.reads++
	return r.selectorSnapshotReader.GetContents(vault, path)
}
