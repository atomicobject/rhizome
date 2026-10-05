package validate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func runOntologyFixture(t *testing.T, files map[string]string) CheckResult {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, files))
	store := openTestStore(t, root)
	vaultDef := obsidian.VaultDefinition{Root: root, Path: root, Links: obsidian.LinkTypeBoth, Includes: []string{"**/*.md"}}
	_, err := testNoteMetadata(t).EnsureIndexed(context.Background(), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	runtime, err := ontology.EnsureFreshRuntimeWithStore(context.Background(), testNoteMetadata(t), vaultDef, &obsidian.Note{}, store)
	require.NoError(t, err)
	runCtx := RunContext{VaultDef: vaultDef, VaultPath: root, NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t), MaxIssues: 100}
	return runOntologyWithRuntime(context.Background(), runCtx, runtime)
}

func parentCycleFixture(parents map[string]string) map[string]string {
	files := map[string]string{
		".rhizome/ontology/schema.graphql": `type Area @node(paths: ["areas/*.md"]) { parent: Area @link @display(role: PARENT) }`,
	}
	for name, parent := range parents {
		frontmatter := "---\ntype: Area\n"
		if parent != "" {
			frontmatter += "parent: \"[[" + parent + "]]\"\n"
		}
		files["areas/"+name+".md"] = frontmatter + "---\n# " + name + "\n"
	}
	return files
}

func parentCycleIssuesOf(result CheckResult) []Issue {
	var out []Issue
	for _, issue := range result.Issues {
		if issue.Code == IssueCodeParentCycle {
			out = append(out, issue)
		}
	}
	return out
}

func TestOntologyReportsEachParentCycleOnce(t *testing.T) {
	result := runOntologyFixture(t, parentCycleFixture(map[string]string{
		"b": "c", "c": "a", "a": "b", // three-record cycle
		"d":    "a", // leads into the cycle but is not part of it
		"self": "self",
		"leaf": "root", "root": "",
	}))
	require.Empty(t, result.Error)

	issues := parentCycleIssuesOf(result)
	require.Len(t, issues, 2, "one issue per cycle: %+v", issues)
	require.Equal(t, "areas/a.md", issues[0].Path)
	require.Equal(t, "Area", issues[0].Type)
	require.Equal(t, "parent", issues[0].Field)
	require.Equal(t, []string{"areas/a.md", "areas/b.md", "areas/c.md"}, issues[0].AffectedNotePaths)
	var data ParentCycleData
	require.NoError(t, json.Unmarshal(issues[0].Data, &data))
	require.Equal(t, []string{"areas/a.md", "areas/b.md", "areas/c.md"}, data.Records)
	require.Contains(t, issues[0].Message, "areas/a.md → areas/b.md → areas/c.md → areas/a.md")

	require.Equal(t, "areas/self.md", issues[1].Path)
	require.Equal(t, []string{"areas/self.md"}, issues[1].AffectedNotePaths)
	require.GreaterOrEqual(t, result.IssueCount, 2, "cycles count toward the ontology issue total")
}

func TestOntologyAcceptsAcyclicParents(t *testing.T) {
	result := runOntologyFixture(t, parentCycleFixture(map[string]string{
		"root": "", "child": "root", "grandchild": "child",
	}))
	require.Empty(t, result.Error)
	require.Empty(t, parentCycleIssuesOf(result))
}
