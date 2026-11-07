package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const productAdmission = `extend type Assertion @admit(matches: ["assertions/alpha OR (NOT assertions/beta AND (products:alpha OR products:\"*\" OR NOT has:products))"])`

func TestAdmissionFromTypeExtension(t *testing.T) {
	root := t.TempDir()
	writeOntologySchema(t, root, `type Assertion @node(paths:["assertions/**/*.md"]){title:String}`)
	plain, err := LoadSchema(root)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/ontology/product.graphql"), []byte(productAdmission), 0o644))
	schema, err := LoadSchema(root)
	require.NoError(t, err)
	require.NotEqual(t, plain.Hash, schema.Hash)
	for _, tc := range []struct {
		path, frontmatter string
		want              bool
	}{
		{"assertions/alpha/claim.md", "products: [beta]", true},
		{"assertions/beta/claim.md", "products: [alpha]", false},
		{"assertions/root.md", "{}", true},
		{"assertions/root.md", "products: null", true},
		{"assertions/root.md", `products: ""`, true},
		{"assertions/root.md", "products: []", false},
		{"assertions/root.md", "products: [alpha]", true},
		{"assertions/root.md", "products: alpha", true},
		{"assertions/root.md", `products: ["*"]`, true},
		{"assertions/root.md", `products: [beta, "*"]`, true},
		{"assertions/root.md", "products: [beta]", false},
		{"other/claim.md", "products: alpha", false},
	} {
		t.Run(tc.path+tc.frontmatter, func(t *testing.T) {
			var frontmatter map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(tc.frontmatter), &frontmatter))
			subject := &Subject{Path: tc.path, Frontmatter: frontmatter}
			require.Equal(t, tc.want, schema.MatchDoc("Assertion", subject))
			require.Equal(t, tc.path != "other/claim.md", plain.MatchDoc("Assertion", subject))
		})
	}
}

func TestAdmissionCompileContract(t *testing.T) {
	for _, tc := range []struct{ name, sdl string }{
		{"record", `type Row @node(locator:EMBEDDED) ` + recordTestSource + ` @admit(matches:["*"]){id:ID @field(path:"id")}`},
		{"embedded", `type Item implements Section @node(locator:EMBEDDED) @admit(matches:["*"]){extra:String}`},
		{"interface", `interface Shared @admit(matches:["*"]){title:String}`},
		{"section", `type Body implements Section @admit(matches:["*"]){extra:String}`},
		{"without node", `type Body @admit(matches:["*"]){title:String}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := recordSchema(t, tc.sdl)
			requireCompileError(t, err, "admit_node_role")
		})
	}
	_, err := recordSchema(t, `type Assertion @node(paths:["*.md"]) @admit(matches:["*"]){title:String} extend type Assertion @admit(matches:["*"])`)
	requireCompileError(t, err, "duplicate_admit")
	_, err = recordSchema(t, `type Assertion @node(paths:["*.md"]) @admit(matches:["products:*"]){title:String}`)
	requireCompileError(t, err, "invalid_admit_matcher")
}

func TestAdmissionEmptyAlternativesAdmitNothing(t *testing.T) {
	schema, err := recordSchema(t, `type Assertion @node(paths:["*.md"]) @admit(matches:[]){title:String}`)
	require.NoError(t, err)
	require.False(t, schema.MatchDoc("Assertion", &Subject{Path: "claim.md"}))
}

func TestAdmissionDoesNotChangeSpecificity(t *testing.T) {
	schema, err := recordSchema(t, `type General @node(paths:["assertions/**/*.md"]){title:String}
type Scoped @node(paths:["assertions/**/*.md"]) @admit(matches:["products:alpha"]){title:String}`)
	require.NoError(t, err)
	subject := &Subject{Path: "assertions/claim.md", Frontmatter: map[string]any{"products": "alpha"}}
	require.True(t, schema.MatchDoc("General", subject))
	require.True(t, schema.MatchDoc("Scoped", subject))
	require.Empty(t, MostSpecificCandidate(subject, []string{"General", "Scoped"}, schema))
}
