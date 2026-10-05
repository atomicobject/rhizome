package validate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunCodeFrontmatterEmitsStructuredDiagnostics(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"docs/anchor.md": `---
code-anchors:
  go:
    - symbol: MissingQualification
---
# Anchor
`,
		"docs/yaml.md": `---
code-anchors: [
---
# YAML
`,
	}))

	result := RunCodeFrontmatter(context.Background(), RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	})

	require.True(t, result.OK)
	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Issues, 2)
	require.Equal(t, []string{"docs/anchor.md", "docs/yaml.md"}, []string{result.Issues[0].Path, result.Issues[1].Path})
	require.Equal(t, "code_anchor_definition_error", result.Issues[0].Code)
	require.Equal(t, "code_frontmatter_yaml_error", result.Issues[1].Code)
	for _, issue := range result.Issues {
		require.Equal(t, "code-anchors", issue.Field)
		require.NotEmpty(t, issue.Message)
		var data map[string]any
		require.NoError(t, json.Unmarshal(issue.Data, &data))
		require.NotEmpty(t, data["diagnostic"])
		require.Contains(t, data["guidance"], "rzm validate code-frontmatter")
	}
	require.Len(t, result.Fixes, 2)
	require.Len(t, result.Fixes[0].IssueKeys, 1)
	require.Len(t, result.Fixes[1].IssueKeys, 1)
	require.NotEqual(t, result.Fixes[0].IssueKeys[0], result.Fixes[1].IssueKeys[0])
	require.Equal(t, "code_anchor_definition_error", result.Fixes[0].IssueCode)
	require.Equal(t, "code_frontmatter_yaml_error", result.Fixes[1].IssueCode)
	require.Equal(t, FixSafetyAgent, result.Fixes[0].Safety)
}

func TestRunCodeFrontmatterSkipsTemplateFrontmatter(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		wantIssues []string
		wantNotes  []string
	}{
		{
			name: "templater and core template placeholders are skipped with a note",
			files: map[string]string{
				"templates/daily.md": "---\ndate: \"<% tp.date.now(\"YYYY-MM-DD\") %>\"\n---\n# Daily\n",
				// Unquoted Templater output is a valid YAML plain scalar, so it is
				// neither reported nor skipped.
				"templates/plain-scalar.md": "---\ndate: <%* tR += tp.date.now(\"YYYY-MM-DD\") %>\n---\n",
				"templates/moved.md":        "---\n<%* await tp.file.move(\"/inbox/\" + tp.file.title) %>\ntitle: x\n---\n",
				"templates/meeting.md":      "---\ntitle: {{title}}\n---\n# Meeting\n",
				"docs/plain.md":             "---\ntitle: fine\n---\n",
			},
			wantNotes: []string{"skipped 3 template note(s) with Templater or {{...}} placeholders in unparseable frontmatter: templates/daily.md, templates/meeting.md, templates/moved.md"},
		},
		{
			name: "genuine YAML errors are still reported",
			files: map[string]string{
				"docs/yaml.md":     "---\ncode-anchors: [\n---\n# YAML\n",
				"docs/body-tpl.md": "---\ntags: [\n---\nBody mentions <% tp.date.now() %> and {{title}} outside frontmatter.\n",
				// Template syntax that is not the cause must not hide the error.
				"templates/also-broken.md": "---\ntitle: \"{{title}}\"\ntags: [a, b\n---\n",
			},
			wantIssues: []string{"docs/body-tpl.md", "docs/yaml.md", "templates/also-broken.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, writeFixtureFiles(root, tt.files))

			result := RunCodeFrontmatter(context.Background(), RunContext{
				VaultDef:     obsidian.VaultDefinition{Path: root},
				VaultPath:    root,
				NoteReader:   &obsidian.Note{},
				NoteMetadata: testNoteMetadata(t),
				MaxIssues:    20,
			})

			require.Empty(t, result.Error)
			var paths []string
			for _, issue := range result.Issues {
				require.Equal(t, issueCodeFrontmatterYAML, issue.Code)
				paths = append(paths, issue.Path)
			}
			require.Equal(t, tt.wantIssues, paths)
			require.Equal(t, len(tt.wantIssues), result.IssueCount)
			require.Equal(t, tt.wantNotes, result.Notes)
		})
	}
}
