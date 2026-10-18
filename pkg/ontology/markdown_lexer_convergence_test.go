package ontology

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/stretchr/testify/require"
)

func TestOntologyLexersRespectExactFenceBoundaries(t *testing.T) {
	for _, fence := range []struct{ name, open, decoy, close string }{
		{"mixed delimiter", "```md", "~~~", "```"},
		{"shorter closer", "````md", "```", "````"},
		{"illegal closer suffix", "~~~md", "~~~ still code", "~~~"},
	} {
		t.Run(fence.name, func(t *testing.T) {
			content := fence.open + "\n## Hidden\nstatus:: hidden\n[[Hidden]]\n- [ ] Hidden #action-item ^hidden\n" + fence.decoy + "\n## Also Hidden\nstatus:: hidden-too\n[[AlsoHidden]]\n" + fence.close + "\n\n## Visible\nstatus:: visible\n\n[[Visible]]\n- [ ] Visible #action-item ^visible\n"
			sections := ParseSections("notes/source.md", content)
			require.Len(t, sections, 1)
			require.Equal(t, "Visible", sections[0].Title)
			props := SectionInlineProperties(content)
			require.Equal(t, []string{"visible"}, props["status"])
			spans := scanInlineFieldsInSegment(content, 0)
			require.Len(t, spans, 1)
			require.Equal(t, "visible", spans[0].Value)
			links := ScanMarkdownBodyLinks(content, markdownsyntax.DefaultWikilinkOptions, markdownsyntax.DefaultMdLinkOptions)
			require.Len(t, links, 1)
			require.Equal(t, "Visible", links[0].Detail.Target)
			nodes := ParseMarkdownListSourceSpans("notes/source.md", content)
			require.Len(t, nodes, 1)
			require.Equal(t, "visible", nodes[0].BlockID)
		})
	}
}
