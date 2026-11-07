package embeddings

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax/sectionparse"
	"github.com/stretchr/testify/require"
)

// The index (sectionparse), search display (noteSectionChunks), and link
// targets (EnumerateHeadings) must agree on ATX headings.
func TestHeadingParsersAgreeOnATXHeadings(t *testing.T) {
	intro := []string{"Intro", "Details"}
	for name, tc := range map[string]struct {
		content  string
		sections []string // index and display; nil means the whole-file fallback
		links    []string
	}{
		"frontmatter comment": {"---\n# comment\ntags: [a]\n---\n# Intro\nbody\n## Details\nmore\n", intro, intro},
		"indented code":       {"# Intro\nbody\n\n    # comment\n\n## Details\nmore\n", intro, intro},
		"fenced code":         {"# Intro\n```sh\n# comment\n```\n## Details\nmore\n", intro, intro},
		"seven hashes":        {"# Intro\n####### seven hashes\n## Details\nmore\n", intro, intro},
		"line-start tag":      {"# Intro\n#todo follow up\n## Details\nmore\n", intro, intro},
		// Known gap: sectionparse is ATX-only, so setext headings are link
		// targets but not index sections, and display falls back to the file.
		"setext": {"Intro\n=====\nbody\n\nDetails\n-------\nmore\n", nil, intro},
	} {
		t.Run(name, func(t *testing.T) {
			var indexed []string
			for _, node := range sectionparse.Flatten(sectionparse.Parse("note.md", tc.content)) {
				indexed = append(indexed, node.Title)
			}
			require.Equal(t, tc.sections, indexed)

			var displayed []string
			for _, chunk := range noteSectionChunks("note.md", "note", tc.content) {
				displayed = append(displayed, chunk.Heading)
			}
			if tc.sections == nil {
				require.Equal(t, []string{"note"}, displayed)
			} else {
				require.Equal(t, tc.sections, displayed)
			}

			var links []string
			for _, heading := range markdownsyntax.EnumerateHeadings(tc.content) {
				links = append(links, heading.Text)
			}
			require.Equal(t, tc.links, links)
		})
	}
}
