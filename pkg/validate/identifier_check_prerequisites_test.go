package validate

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const identifierPrerequisiteSchema = `
type Spec @node(paths: ["specs/*.md"], label: "Spec", propertyCase: AS_DEFINED) {
  specId: String! @field @identifier(preferred: true, prefix: "SPEC", separator: "-", pad: 3)
}
`

func TestRunIdentifiersWithRuntimeSkipsWithoutPrerequisites(t *testing.T) {
	_, noIdentifierRuntime := identifierCheckRuntime(t,
		`type Spec @node(paths: ["specs/*.md"], label: "Spec") { title: String @field }`,
		map[string]string{"specs/one.md": "---\ntype: Spec\ntitle: One\n---\n# One\n"})
	_, storeRuntime := identifierCheckRuntime(t, identifierPrerequisiteSchema, map[string]string{
		"specs/one.md": "---\ntype: Spec\nspecId: SPEC-001\n---\n# One\n",
	})
	noStoreRuntime := &ontology.Runtime{Schema: storeRuntime.Schema}

	tests := []struct {
		name    string
		runtime *ontology.Runtime
		summary string
	}{
		{name: "nil runtime", runtime: nil, summary: "no ontology schema"},
		{name: "missing schema", runtime: &ontology.Runtime{Store: storeRuntime.Store}, summary: "no ontology schema"},
		{name: "missing store", runtime: noStoreRuntime, summary: "no intel store"},
		{name: "no identifier fields", runtime: noIdentifierRuntime, summary: "no @identifier fields"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RunIdentifiersWithRuntime(context.Background(), RunContext{MaxIssues: 100}, tt.runtime)
			assert.Equal(t, CheckIdentifiers, result.Name)
			assert.True(t, result.OK)
			assert.True(t, result.Skipped)
			assert.Equal(t, tt.summary, result.Summary)
			assert.Empty(t, result.Error)
			assert.Zero(t, result.IssueCount)
			assert.Empty(t, result.Fixes)
		})
	}
}

func TestRunIdentifiersWithRuntimeReportsMissingAliasMirror(t *testing.T) {
	t.Run("mirrored identifier is clean", func(t *testing.T) {
		result := runIdentifierPrerequisiteFixture(t, map[string]string{
			"specs/one.md": "---\ntype: Spec\nspecId: SPEC-001\naliases: [SPEC-001]\n---\n# One\n",
		})
		assert.True(t, result.OK)
		assert.False(t, result.Skipped)
		assert.Zero(t, result.IssueCount)
		assert.Empty(t, result.Issues)
		assert.Empty(t, result.Fixes)
	})

	t.Run("missing mirror exposes safe append_alias", func(t *testing.T) {
		result := runIdentifierPrerequisiteFixture(t, map[string]string{
			"specs/one.md": "---\ntype: Spec\nspecId: SPEC-001\n---\n# One\n",
		})
		assert.True(t, result.OK)
		require.Equal(t, 1, result.IssueCount)
		require.Len(t, result.Issues, 1)
		issue := result.Issues[0]
		assert.Equal(t, "identifier_not_in_aliases", issue.Code)
		assert.Equal(t, "specs/one.md", issue.Path)
		assert.Equal(t, "specId", issue.Field)
		assert.Equal(t, "SPEC-001", issue.Target)
		assert.Equal(t, "specid", issue.Source)
		assert.Equal(t, &IssueVariant{Key: "Spec.specId", Label: "Spec · specId"}, issue.Variant)
		require.NotEmpty(t, issue.Key)

		require.Len(t, result.Fixes, 1)
		fix := result.Fixes[0]
		assert.Equal(t, "alias-mirror:specs/one.md:specId:SPEC-001", fix.ID)
		assert.Equal(t, CheckAliases, fix.Check)
		assert.Equal(t, "identifier_not_in_aliases", fix.IssueCode)
		assert.Equal(t, FixKindAppendAlias, fix.Kind)
		assert.Equal(t, FixSafetySafe, fix.Safety)
		assert.Equal(t, []string{issue.Key}, fix.IssueKeys)
		assert.Equal(t, []string{"specs/one.md"}, fix.AffectedPaths)
		assert.Equal(t, []FixEdit{{Kind: FixKindAppendAlias, NotePath: "specs/one.md", Value: "SPEC-001"}}, fix.Edits)
	})
}

func TestRunIdentifiersWithRuntimeGeneratesIdentifierFromPathWithConfirmation(t *testing.T) {
	result := runIdentifierPrerequisiteFixture(t, map[string]string{
		"specs/spec-002.md": "---\ntype: Spec\n---\n# Two\n",
	})

	require.Equal(t, 1, result.IssueCount)
	require.Len(t, result.Issues, 1)
	issue := result.Issues[0]
	require.Equal(t, "missing_required_field", issue.Code)
	require.Equal(t, "specs/spec-002.md", issue.Path)
	require.Equal(t, "specId", issue.Field)
	require.NotEmpty(t, issue.Key)
	fix := requireIdentifierFix(t, result, "identifier-from-path:specs/spec-002.md:specId")
	assert.Equal(t, []string{issue.Key}, fix.IssueKeys)
	assert.Equal(t, FixSafetyConfirm, fix.Safety)
	assert.Equal(t, `Set specId on specs/spec-002.md to "SPEC-002"?`, fix.Question)
	assert.Equal(t, []FixEdit{{Kind: FixKindSetFrontmatter, NotePath: "specs/spec-002.md", Property: "specId", Value: "SPEC-002"}}, fix.Edits)
}

func TestRunIdentifiersWithRuntimeGeneratesIdentifiersFromAliasesInPathOrder(t *testing.T) {
	result := runIdentifierPrerequisiteFixture(t, map[string]string{
		"specs/b.md": "---\ntype: Spec\naliases: [SPEC-002]\n---\n# B\n",
		"specs/a.md": "---\ntype: Spec\naliases: [SPEC-001]\n---\n# A\n",
	})

	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Issues, 2)
	assert.Equal(t, "specs/a.md", result.Issues[0].Path)
	assert.Equal(t, "specs/b.md", result.Issues[1].Path)
	require.Len(t, result.Fixes, 2)
	for index, want := range []struct{ path, value string }{{"specs/a.md", "SPEC-001"}, {"specs/b.md", "SPEC-002"}} {
		fix := result.Fixes[index]
		assert.Equal(t, "identifier-from-alias:"+want.path+":specId", fix.ID)
		assert.Equal(t, []string{result.Issues[index].Key}, fix.IssueKeys)
		assert.Equal(t, FixSafetySafe, fix.Safety)
		assert.Equal(t, []FixEdit{{Kind: FixKindSetFrontmatter, NotePath: want.path, Property: "specId", Value: want.value}}, fix.Edits)
	}
}

// The reserved and unreserved fixtures differ only in the existing note's
// identifier, so a missing candidate fix can only come from the reservation.
func TestRunIdentifiersWithRuntimeDoesNotGenerateSemanticallyReservedIdentifier(t *testing.T) {
	tests := []struct {
		name         string
		existingID   string
		candidate    string
		candidateFix string
		value        string
	}{
		{name: "case folded alias", existingID: "SPEC-001", candidate: "specs/missing.md", candidateFix: "identifier-from-alias:specs/missing.md:specId", value: "spec-001"},
		{name: "caret stripped alias", existingID: "SPEC-001", candidate: "specs/missing.md", candidateFix: "identifier-from-alias:specs/missing.md:specId", value: "^SPEC-001"},
		{name: "case folded caret path", existingID: "^spec-001", candidate: "specs/spec-001.md", candidateFix: "identifier-from-path:specs/spec-001.md:specId", value: "SPEC-001"},
	}
	for _, tt := range tests {
		for _, reserved := range []bool{true, false} {
			existingID := tt.existingID
			if !reserved {
				existingID = "SPEC-009"
			}
			name := tt.name + "/unreserved control"
			if reserved {
				name = tt.name + "/reserved"
			}
			t.Run(name, func(t *testing.T) {
				candidate := "---\ntype: Spec\n---\n# Missing\n"
				if tt.candidate == "specs/missing.md" {
					candidate = "---\ntype: Spec\naliases: [\"" + tt.value + "\"]\n---\n# Missing\n"
				}
				result := runIdentifierPrerequisiteFixture(t, map[string]string{
					"specs/existing.md": "---\ntype: Spec\nspecId: \"" + existingID + "\"\naliases: [\"" + existingID + "\"]\n---\n# Existing\n",
					tt.candidate:        candidate,
				})

				require.True(t, result.OK)
				require.False(t, result.Skipped)
				require.Empty(t, result.Error)
				missing := identifierIssuesWithCode(result, "missing_required_field")
				require.Len(t, missing, 1)
				require.Equal(t, tt.candidate, missing[0].Path)
				require.Equal(t, "specId", missing[0].Field)

				var found *FixAction
				for index := range result.Fixes {
					if result.Fixes[index].ID == tt.candidateFix {
						found = &result.Fixes[index]
					}
				}
				if reserved {
					require.Nil(t, found, "a semantically reserved identifier must not be generated")
					return
				}
				require.NotNil(t, found)
				assert.Equal(t, []string{missing[0].Key}, found.IssueKeys)
				assert.Equal(t, []FixEdit{{Kind: FixKindSetFrontmatter, NotePath: tt.candidate, Property: "specId", Value: tt.value}}, found.Edits)
			})
		}
	}
}

func runIdentifierPrerequisiteFixture(t *testing.T, files map[string]string) CheckResult {
	t.Helper()
	root, runtime := identifierCheckRuntime(t, identifierPrerequisiteSchema, files)
	return RunIdentifiersWithRuntime(context.Background(), RunContext{
		VaultDef:   obsidian.VaultDefinition{Path: root},
		VaultPath:  root,
		NoteReader: &obsidian.Note{},
		MaxIssues:  100,
	}, runtime)
}

func requireIdentifierFix(t *testing.T, result CheckResult, id string) FixAction {
	t.Helper()
	for _, fix := range result.Fixes {
		if fix.ID == id {
			return fix
		}
	}
	require.Failf(t, "missing fix", "no fix %q in %#v", id, result.Fixes)
	return FixAction{}
}

func identifierIssuesWithCode(result CheckResult, code string) []Issue {
	var issues []Issue
	for _, issue := range result.Issues {
		if issue.Code == code {
			issues = append(issues, issue)
		}
	}
	return issues
}
