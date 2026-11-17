package schema

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/stretchr/testify/require"
)

func compileSDLWithCase(t *testing.T, sdl string, pathCase paths.CasePolicy) *Schema {
	t.Helper()
	root := t.TempDir()
	writeOntologySchema(t, root, sdl)
	authored, hash, err := readAuthoredSchemaFiles(root)
	require.NoError(t, err)
	schema, err := compileSchema(root, authored, hash, pathCase)
	require.NoError(t, err)
	require.Equal(t, pathCase, schema.PathCase)
	return schema
}

const aoLikeSDL = `
type MapOfContent @node(paths: ["_moc.md", "MOCs/**/*.md", "products/*/MOCs/**/*.md"]) { title: String }
type Rider @node(paths: ["mocs/Riders/**/*.md"]) { title: String }
type Twin @node(paths: ["mocs/**/*.md"]) { title: String }
type Scoped @node(matches: ["MOCs AND tag:scoped"]) @admit(matches: ["MOCs"]) { title: String }
`

func TestPathSelectorsFollowTheRootCasePolicy(t *testing.T) {
	t.Parallel()
	riders := &Subject{Path: "mocs/riders.md", Tags: []string{}}
	scoped := &Subject{Path: "mocs/plan.md", Tags: []string{"scoped"}}

	fold := compileSDLWithCase(t, aoLikeSDL, paths.CaseFold)
	require.True(t, fold.MatchDoc("MapOfContent", riders), "fold: MOCs/**/*.md selects mocs/riders.md")
	require.True(t, fold.MatchDoc("Scoped", scoped), "fold: file prefix MOCs and @admit MOCs accept mocs/")
	require.False(t, fold.MatchDoc("MapOfContent", &Subject{Path: "docs/riders.md"}), "fold never matches different letters")
	require.Nil(t, fold.Types["mapofcontent"], "type names stay case sensitive")
	require.NotNil(t, fold.Types["MapOfContent"])

	exact := compileSDLWithCase(t, aoLikeSDL, paths.CaseExact)
	require.False(t, exact.MatchDoc("MapOfContent", riders), "exact: MOCs/**/*.md does not select mocs/riders.md")
	require.False(t, exact.MatchDoc("Scoped", scoped))
	require.True(t, exact.MatchDoc("MapOfContent", &Subject{Path: "MOCs/riders.md"}))
	require.NotEqual(t, fold.Hash, exact.Hash, "the schema witness records the policy")
}

func TestSpecificityAgreesWithTheCasePolicy(t *testing.T) {
	t.Parallel()
	doc := &Subject{Path: "mocs/riders/lead.md", Tags: []string{}}

	fold := compileSDLWithCase(t, aoLikeSDL, paths.CaseFold)
	require.Equal(t, "Rider", MostSpecificCandidate(doc, []string{"MapOfContent", "Rider"}, fold), "fold: mocs/Riders/ refines MOCs/")
	require.Equal(t, "", MostSpecificCandidate(doc, []string{"MapOfContent", "Twin"}, fold), "fold: selectors differing only in case tie")
	require.Equal(t, "Rider", MostSpecificCandidate(doc, []string{"Twin", "Rider"}, fold))

	exact := compileSDLWithCase(t, aoLikeSDL, paths.CaseExact)
	require.False(t, exact.MatchDoc("MapOfContent", doc), "exact: MOCs/ never selects mocs/")
	require.False(t, exact.MatchDoc("Rider", doc), "exact: mocs/Riders/ never selects mocs/riders/")
	require.Equal(t, "Rider", MostSpecificCandidate(&Subject{Path: "mocs/Riders/lead.md"}, []string{"Twin", "Rider"}, exact), "exact: byte-equal prefixes still refine")
}
