package query

import (
	"context"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestInboundSectionNeighborsUseProviderLinksForMarkdownSources(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		suppress bool
		want     map[string][]string
	}{
		{"provider replacement", false, map[string][]string{"target.md\x00block:provider": {"source.md"}}},
		{"authoritative empty links", true, map[string][]string{}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			env := newCustomQueryTestEnv(t, `type Document @node(paths: ["**/*.md"]) { title: String @field }`, map[string]string{
				"source.md":    "# Source\n[[target#^example]]\n",
				"target.md":    "# Target\n## Example\n^example\n## Provider\n^provider\n",
				"unrelated.md": "# Unrelated\n",
			})
			provider := inboundProviderFactsProjector{suppress: fixture.suppress}
			registry, err := noteformat.NewRegistry(provider)
			require.NoError(t, err)
			formats, err := noteformat.NewRuntime(registry, provider)
			require.NoError(t, err)
			reader := &inboundProviderFactsReader{Note: &obsidian.Note{}, reads: map[string]int{}}
			deps := env.deps(nil)
			deps.NoteFormats, deps.NoteReader = formats, reader
			loaders, err := newLoaders(deps, env.schema)
			require.NoError(t, err)
			exec := &executor{deps: deps, loaders: loaders, schema: env.schema}

			// The selected provider classifies the raw example independently.
			// Consumers must use its link facts, including an empty set.
			neighbors, err := exec.inboundSectionNeighborIndex(context.Background(), "target.md")
			require.NoError(t, err)
			require.Equal(t, fixture.want, neighbors)
			require.Equal(t, 1, reader.readCount("source.md"))
			require.Zero(t, reader.readCount("unrelated.md"))
			_, err = exec.inboundSectionNeighborIndex(context.Background(), "target.md")
			require.NoError(t, err)
			require.Equal(t, 1, reader.readCount("source.md"))
		})
	}
}

type inboundProviderFactsProjector struct{ suppress bool }

func (inboundProviderFactsProjector) Descriptor() noteformat.Descriptor {
	return markdown.New().Descriptor()
}
func (p inboundProviderFactsProjector) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	projection, err := markdown.New().Project(source)
	if source.Path().String() == "source.md" {
		if p.suppress {
			projection.Facts.Links = nil
		} else {
			projection.Facts.Links[0].Fragment = "^provider"
			projection.Facts.Links[0].Target = "target#^provider"
			projection.Facts.Links[0].ResolverInput = "target#^provider"
		}
	}
	return projection, err
}

type inboundProviderFactsReader struct {
	*obsidian.Note
	mu    sync.Mutex
	reads map[string]int
}

func (r *inboundProviderFactsReader) GetContents(vault obsidian.VaultDefinition, path string) (string, error) {
	r.mu.Lock()
	r.reads[path]++
	r.mu.Unlock()
	return r.Note.GetContents(vault, path)
}
func (r *inboundProviderFactsReader) readCount(path string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads[path]
}
