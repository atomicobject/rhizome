package ontology

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAdmissionExtensionRefreshConverges(t *testing.T) {
	files := map[string]string{
		".rhizome/ontology/schema.graphql": `type Host @node(default:true){status:String}
type Assertion @node(paths:["assertions/**/*.md"]){aoId:String @field(source:"id")}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model.yaml"],select:"rows",key:{field:"id"},title:["id"],space:"selected",logicalKind:"row"){
 evidence:[String!] @field(path:"evidence") @references(to:["Assertion"],targetKey:"aoId",mode:STRICT,values:ELEMENTS,edge:{name:"evidenced-by"})
}`,
		"model.yaml":                "rows: [{id: M, evidence: [shared]}]\n",
		"assertions/alpha/claim.md": "---\nid: shared\nproducts: [beta]\n---\n# Alpha\n",
		"assertions/beta/claim.md":  "---\nid: shared\nproducts: [alpha]\n---\n# Beta\n",
	}
	cases := []struct {
		frontmatter string
		alpha, beta bool
	}{
		{"", true, true}, {"products: null\n", true, true}, {"products: \"\"\n", true, true},
		{"products: []\n", false, false}, {"products: [alpha]\n", true, false}, {"products: alpha\n", true, false},
		{"products: [\"*\"]\n", true, true}, {"products: [beta, \"*\"]\n", true, true}, {"products: [beta]\n", false, true},
	}
	for i, tc := range cases {
		files[fmt.Sprintf("assertions/root%d.md", i)] = fmt.Sprintf("---\nid: root%d\n%s---\n# Claim\n", i, tc.frontmatter)
	}
	root := testkit.Vault(t, files)
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"assertions/**/*.md", "model.yaml"}}
	indexer := testkit.NoteMetadataIndexer(t)
	store, err := semdb.Open(filepath.Join(t.TempDir(), "incremental.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	previous, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	for _, selected := range []string{"alpha", "beta"} {
		t.Run(selected, func(t *testing.T) {
			excluded := "beta"
			if selected == "beta" {
				excluded = "alpha"
			}
			extension := fmt.Sprintf(`extend type Assertion @admit(matches:["assertions/%s OR (NOT assertions/%s AND (products:%s OR products:\"*\" OR NOT has:products))"])`, selected, excluded, selected)
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ontology/product.graphql"), []byte(extension), 0o644))
			_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, nil, nil)
			require.NoError(t, err)
			current, err := ontoschema.LoadSchema(root)
			require.NoError(t, err)
			require.NotEqual(t, previous.Hash, current.Hash)
			previous = current
			assertStructuredPublicationConverges(t, indexer, vault, store)
			var paths []string
			for i := range cases {
				paths = append(paths, fmt.Sprintf("assertions/root%d.md", i))
			}
			paths = append(paths, "assertions/alpha/claim.md", "assertions/beta/claim.md")
			assessments, err := store.OntologyAssessmentsByPaths(t.Context(), paths)
			require.NoError(t, err)
			assertType := func(path string, admitted bool) {
				want := "Host"
				if admitted {
					want = "Assertion"
				}
				require.Equal(t, want, assessments[path].ResolvedType, path)
				assessment, err := AssessmentFromJSON(assessments[path].AssessmentJSON)
				require.NoError(t, err)
				if admitted {
					require.Equal(t, []string{"Assertion"}, assessment.SelectorCandidateTypes)
				} else {
					require.Empty(t, assessment.SelectorCandidateTypes)
				}
			}
			for i, tc := range cases {
				admitted := tc.alpha
				if selected == "beta" {
					admitted = tc.beta
				}
				assertType(paths[i], admitted)
			}
			assertType("assertions/alpha/claim.md", selected == "alpha")
			assertType("assertions/beta/claim.md", selected == "beta")
			edges, err := store.OntologyEdgesForPaths(t.Context(), []string{"model.yaml"}, true, "evidenced-by", 0)
			require.NoError(t, err)
			require.Len(t, edges, 1)
			require.Equal(t, "assertions/"+selected+"/claim.md", edges[0].DstPath)
		})
	}
}

func TestDefaultTypeHonorsAdmission(t *testing.T) {
	schema := recordFixtureSchema(t, `type Host @node(default:true) @admit(matches:["products:alpha"]){status:String}`)
	require.Nil(t, resolveNoteAssessment(&noteDoc{Path: "note.md"}, schema))
	assessment := resolveNoteAssessment(&noteDoc{Path: "note.md", Frontmatter: map[string]any{"products": "alpha"}}, schema)
	require.Equal(t, "Host", assessment.ResolvedType)
}
