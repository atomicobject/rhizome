package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunFragileExternal_ClassifiesHeadingRefs(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n\n## Current Heading\n\n## Duplicate\n\n## Duplicate\n",
		"source.md": "# Source\n\n[[target#Current Heading]]\n[[target#Old Heading]]\n[[target#Duplicate]]\n",
	}))
	runCtx := RunContext{
		VaultDef:     obsidian.VaultDefinition{Name: "test", Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}

	result := RunFragileExternal(context.Background(), runCtx, Options{})

	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Issues, 2)
	assert.Contains(t, result.Issues[0].Message, "Old Heading")
	assert.Contains(t, result.Issues[1].Message, "ambiguous")
}

func TestApplyFragileExternalFixUsesSourceRange(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		".rhizome/ontology/schema.graphql": `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
}

type Stories implements Section {
  stories: [UserStory!] @contains(level: H3)
}

type Spec @node(paths: ["specs/*.md"]) {
  summary: String!
  stories: Stories @contains(level: H2, heading: "Stories")
}
`,
		"specs/one.md": `---
type: Spec
summary: One
---
# One

## Stories

### Old Heading
id:: US-1
`,
		"notes/ref.md": "# Ref\n\n```md\n[[one#Old Headng]]\n```\n\n[[one#Old Headng]]\n",
	}))
	runCtx := RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}

	result := RunFragileExternal(context.Background(), runCtx, Options{})
	require.Equal(t, 1, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	applyCheckFixThroughPlan(t, runCtx, result, result.Fixes[0])

	ref, err := os.ReadFile(filepath.Join(root, "notes/ref.md"))
	require.NoError(t, err)
	assert.Contains(t, string(ref), "```md\n[[one#Old Headng]]\n```")
	assert.Contains(t, string(ref), "[[one#^US-1]]")
}
