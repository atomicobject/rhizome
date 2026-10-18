package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSchema_RejectsInvalidNeighborFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  decision: Decision @neighbors(direction: BOTH, type: "Decision")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	requireCompileError(t, err, "neighbor_list")
}

func TestLoadSchema_RejectsInverseThatIsNotLinkField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Team @node(paths: ["teams/*.md"]) {
  name: String!
}

type Person @node(paths: ["people/*.md"]) {
  team: Team @link(inverse: "name")
}
`), 0o644))

	_, err := LoadSchema(root)
	require.Error(t, err)
	requireCompileError(t, err, "inverse_field_kind")
}

func TestLoadSchema_ReverseRequiresNamedAuthoredLink(t *testing.T) {
	for _, tc := range []struct{ name, field, source, code string }{
		{"valid", `evidence: [Evidence!] @reverse(field: "opportunities")`, `opportunities: [Opportunity!] @link`, ""},
		{"missing source", `evidence: [Evidence!] @reverse(field: "missing")`, `opportunities: [Opportunity!] @link`, "reverse_source_link"},
		{"wrong source kind", `evidence: [Evidence!] @reverse(field: "opportunities")`, `opportunities: [Opportunity!] @neighbors(direction: OUTBOUND, type: "Opportunity")`, "reverse_source_link"},
		{"singular", `evidence: Evidence @reverse(field: "opportunities")`, `opportunities: [Opportunity!] @link`, "reverse_list"},
		{"mixed contains", `evidence: [Evidence!] @contains(level: H2) @reverse(field: "opportunities")`, `opportunities: [Opportunity!] @link`, "contains_reverse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".rhizome", "ontology")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			body := `type Opportunity @node(paths: ["opportunities/*.md"]) { ` + tc.field + ` }
type Evidence @node(paths: ["evidence/*.md"]) { ` + tc.source + ` }`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "reverse.graphql"), []byte(body), 0o644))
			_, err := LoadSchema(root)
			if tc.code == "" {
				require.NoError(t, err)
			} else {
				requireCompileError(t, err, tc.code)
			}
		})
	}
}

func TestLoadSchema_ReverseAcceptsEmbeddedSourcesCompiledLater(t *testing.T) {
	for _, tc := range []struct {
		name, target, declaration string
	}{
		{
			name:   "concrete embedded node",
			target: "ZEmbedded",
			declaration: `type ZEmbedded implements Section @node(locator: EMBEDDED) {
  parent: Parent @link
}`,
		},
		{
			name:   "Section-derived interface",
			target: "ZEmbeddedInterface",
			declaration: `interface ZEmbeddedInterface implements Section {
  parent: Parent @link
}
type ZEmbedded implements ZEmbeddedInterface & Section @node(locator: EMBEDDED) {
  parent: Parent @link
}`,
		},
		{
			name:   "note interface",
			target: "ZEvidence",
			declaration: `interface ZEvidence {
  parent: Parent @link
}
type ZEvidenceNote implements ZEvidence @node(paths: ["evidence/*.md"]) {
  parent: Parent @link
}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologySchema(t, root, `type Parent @node(paths: ["parents/*.md"]) {
  children: [`+tc.target+`!] @reverse(field: "parent")
}
`+tc.declaration)
			_, err := LoadSchema(root)
			require.NoError(t, err)
		})
	}
}

func TestLoadSchema_RejectsInverseWithWrongTargetType(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.graphql"), []byte(`
type Project @node(paths: ["projects/*.md"]) {
  name: String
}

type Team @node(paths: ["teams/*.md"]) {
  project: Project @link
}

type Person @node(paths: ["people/*.md"]) {
  team: Team @link(inverse: "project")
}
`), 0o644))

	_, err := LoadSchema(root)
	requireCompileError(t, err, "inverse_field_target")
}
