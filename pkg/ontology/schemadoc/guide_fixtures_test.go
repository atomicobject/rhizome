package schemadoc

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"

	"testing"
)

func testSchema(t *testing.T) *ontoschema.Schema {
	t.Helper()
	return testCustomSchema(t, `
enum ProjectStatus {
  ACTIVE
  PAUSED
}

type Team @node(paths: ["notes/teams/*.md"]) {
  name: String!
  members: [Person!] @link(inverse: "team")
}

type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
  role: String @field(source: "role", sourceKind: INLINE)
  team: Team @link(inverse: "members")
  projects: [Project!] @link(inverse: "owner")
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

"""Project note doc."""
type Project
  @node(paths: ["notes/projects/*.md"])
  @retrieval(intents: ["search"], boost: 1.2) {
  name: String!
  status: ProjectStatus!
  owner: Person! @link(inverse: "projects")
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}
`)
}

func testCustomSchema(t *testing.T, body string) *ontoschema.Schema {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(body), 0o644))
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	return schema
}
