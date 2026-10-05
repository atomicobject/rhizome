package validate

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunViewsValidatesDisplayGroupMounts(t *testing.T) {
	root := t.TempDir()
	writeValidationTestFile(t, root+"/.rhizome/ontology/schema.graphql", `type Effort @node(paths: ["efforts/*.md"]) @display(group: "Delivery") { summary: String }`)
	writeValidationTestFile(t, root+"/.rhizome/views/dashboard/index.html", "<!doctype html><title>Dashboard</title>")
	writeValidationTestFile(t, root+"/.rhizome/views/dashboard/view.yaml", `apiVersion: rhizome.view.v1
id: dashboard
name: Dashboard
source:
  kind: custom
  entry: index.html
mount:
  kind: group
  group: Missing
`)
	result := RunViews(context.Background(), RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}, MaxIssues: 20})
	require.False(t, result.OK)
	requireIssue(t, result.Issues, "invalid_mount_target")
}
