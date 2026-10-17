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

	result := RunFragileExternal(context.Background(), runCtx, ScanOptions{})

	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Issues, 2)
	assert.Contains(t, result.Issues[0].Message, "Old Heading")
	assert.Contains(t, result.Issues[1].Message, "ambiguous")
}

func TestApplyFragileExternalFixUsesSourceRange(t *testing.T) {
	runCtx := fragileExternalRepairFixture(t, "# Ref\n\n```md\n[[one#Old Headng]]\n```\n\n[[one#Old Headng]]\n", "Old Heading")
	result := RunFragileExternal(context.Background(), runCtx, ScanOptions{})
	require.Equal(t, 1, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	require.Len(t, result.Fixes[0].Edits, 1)
	assert.Equal(t, "one#Old Headng", result.Fixes[0].Edits[0].OldTarget)
	assert.Equal(t, 37, result.Fixes[0].Edits[0].StartByte)
	assert.Equal(t, 55, result.Fixes[0].Edits[0].EndByte)
	applyCheckFixThroughPlan(t, runCtx, result, result.Fixes[0])

	ref, err := os.ReadFile(filepath.Join(runCtx.VaultPath, "notes/ref.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Ref\n\n```md\n[[one#Old Headng]]\n```\n\n[[one#^US-1]]\n", string(ref))
}

func TestApplyFragileExternalFixPreservesEmbedAndDisplaySyntax(t *testing.T) {
	const protected = "# Ref\n\n```md\n[[one#Old Headng]]\n```\n\n"
	const protectedTail = "\n`![[one#Old Headng|inline]]`\n\n```md\n![[one#Old Headng|示例]]\n[[one#Old Headng|alias]]\n```\n"
	for _, tc := range []struct {
		name, before, after string
		starts, ends        []int
	}{
		{name: "embed", before: "![[one#Old Headng]]\n", after: "![[one#^US-1]]\n", starts: []int{38}, ends: []int{56}},
		{name: "alias", before: "[[one#Old Headng|label]]\n", after: "[[one#^US-1|label]]\n", starts: []int{37}, ends: []int{61}},
		{name: "embed alias", before: "![[one#Old Headng|label]]\n", after: "![[one#^US-1|label]]\n", starts: []int{38}, ends: []int{62}},
		{name: "unicode byte offsets", before: "前言🙂\n![[one#Old Headng|展示✨]]\n", after: "前言🙂\n![[one#^US-1|展示✨]]\n", starts: []int{49}, ends: []int{77}},
		{name: "mixed", before: "![[one#Old Headng]]\n[[one#Old Headng]]\n[[one#Old Headng|label]]\n", after: "![[one#^US-1]]\n[[one#^US-1]]\n[[one#^US-1|label]]\n", starts: []int{38, 57, 76}, ends: []int{56, 75, 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runCtx := fragileExternalRepairFixture(t, protected+tc.before+protectedTail, "Old Heading")
			result := RunFragileExternal(t.Context(), runCtx, ScanOptions{})
			require.Equal(t, len(tc.starts), result.IssueCount)
			require.Len(t, result.Fixes, len(tc.starts))
			for index, fix := range result.Fixes {
				require.Len(t, fix.Edits, 1)
				assert.Equal(t, "one#Old Headng", fix.Edits[0].OldTarget)
				assert.Equal(t, tc.starts[index], fix.Edits[0].StartByte)
				assert.Equal(t, tc.ends[index], fix.Edits[0].EndByte)
			}
			plan, err := BuildRepairPlan(t.Context(), runCtx, []CheckResult{result})
			require.NoError(t, err)
			byAction := make(map[string]FixEdit, len(result.Fixes))
			for _, fix := range result.Fixes {
				byAction[fix.ID] = fix.Edits[0]
			}
			for _, operation := range plan.Operations {
				if operation.Path != "notes/ref.md" {
					continue
				}
				edit := byAction[operation.ActionID]
				require.Len(t, operation.Expected, 1)
				span := operation.Expected[0]
				assert.Equal(t, edit.StartByte+6, span.StartByte)
				assert.Equal(t, edit.StartByte+16, span.EndByte)
				assert.Equal(t, "Old Headng", span.Text)
				assert.Equal(t, "^US-1", span.Replacement)
				before := protected + tc.before + protectedTail
				assert.Equal(t, SourceHash([]byte(before)), operation.SourceHash)
				assert.Equal(t, before[:span.StartByte]+"^US-1"+before[span.EndByte:], string(operation.Content))
			}
			for _, transaction := range plan.Transactions {
				require.Empty(t, transaction.Conflicts)
			}
			execution, err := ApplyFixPlan(t.Context(), runCtx, plan, ApplyOptions{Fix: true, Confirm: func(string) (bool, error) { return true, nil }})
			require.NoError(t, err)
			require.Len(t, execution.Applied, len(result.Fixes))
			ref, err := os.ReadFile(filepath.Join(runCtx.VaultPath, "notes/ref.md"))
			require.NoError(t, err)
			assert.Equal(t, protected+tc.after+protectedTail, string(ref))
		})
	}
}

func TestFragileExternalOverlappingOccurrencesRemainConflicted(t *testing.T) {
	for _, tc := range []struct {
		name, before, heading string
		span                  ExpectedText
	}{
		{
			name: "nonempty replacement", before: "# Ref\n\n![[one#Old Headng|label]]\n", heading: "Old Heading",
			span: ExpectedText{StartByte: 14, EndByte: 24, Text: "Old Headng", Replacement: "^US-1"},
		},
		{
			name: "zero width insertion", before: "# Ref\n\n[[one#S-1]]\n", heading: "US-1",
			span: ExpectedText{StartByte: 13, EndByte: 13, Replacement: "^U"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runCtx := fragileExternalRepairFixture(t, tc.before, tc.heading)
			result := RunFragileExternal(t.Context(), runCtx, ScanOptions{})
			require.Len(t, result.Fixes, 1)
			duplicate := result.Fixes[0]
			duplicate.ID += ":overlap"
			result.Fixes = append(result.Fixes, duplicate)
			targetBefore, err := os.ReadFile(filepath.Join(runCtx.VaultPath, "specs/one.md"))
			require.NoError(t, err)
			plan, err := BuildRepairPlan(t.Context(), runCtx, []CheckResult{result})
			require.NoError(t, err)
			for _, operation := range plan.Operations {
				if operation.Path == "notes/ref.md" {
					assert.Equal(t, []ExpectedText{tc.span}, operation.Expected)
				}
			}
			require.Len(t, plan.Transactions, 1)
			if assert.NotEmpty(t, plan.Transactions[0].Conflicts) {
				assert.Equal(t, RepairConflictEditOverlap, plan.Transactions[0].Conflicts[0].Kind)
				assert.Equal(t, "notes/ref.md", plan.Transactions[0].Conflicts[0].Path)
			}
			execution, err := ApplyFixPlan(t.Context(), runCtx, plan, ApplyOptions{Fix: true, Confirm: func(string) (bool, error) { return true, nil }})
			require.NoError(t, err)
			assert.Empty(t, execution.Applied)
			require.Len(t, execution.Transactions, 1)
			assert.Equal(t, "skipped_conflict", execution.Transactions[0].Status)
			ref, err := os.ReadFile(filepath.Join(runCtx.VaultPath, "notes/ref.md"))
			require.NoError(t, err)
			assert.Equal(t, tc.before, string(ref))
			target, err := os.ReadFile(filepath.Join(runCtx.VaultPath, "specs/one.md"))
			require.NoError(t, err)
			assert.Equal(t, targetBefore, target)
			evidence, err := DetectPendingRepairJournals(runCtx)
			require.NoError(t, err)
			assert.Empty(t, evidence)
		})
	}
}

func fragileExternalRepairFixture(t *testing.T, source, heading string) RunContext {
	t.Helper()
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

### ` + heading + `
id:: US-1
`,
		"notes/ref.md": source,
	}))
	return RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}

}
