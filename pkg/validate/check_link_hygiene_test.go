package validate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunLinkHygieneFindsAndFixesResolvableTargets(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"docs/feature-areas/organization-and-onboarding-access.md": `---
aliases:
  - FA-0001
---
# Organization and onboarding access
`,
		"docs/specs/product/spec.md": `# Spec

- bad wikilink extension: [[docs/feature-areas/organization-and-onboarding-access.md|FA-0001]]
- bad markdown scope: [FA-0001](/docs/feature-areas/organization-and-onboarding-access.md)
`,
	}))
	runCtx := linkHygieneRunContext(t, root)

	result := RunLinkHygiene(runCtx)

	require.Equal(t, 2, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	require.Empty(t, result.Fixes[0].IssueCode, "a mixed-code batch must not invent an issue code")
	require.Len(t, result.Fixes[0].IssueKeys, 2)
	applyCheckFixThroughPlan(t, runCtx, result, result.Fixes[0])

	updated, err := os.ReadFile(filepath.Join(root, "docs/specs/product/spec.md"))
	require.NoError(t, err)
	assert.Contains(t, string(updated), "[[docs/feature-areas/organization-and-onboarding-access|FA-0001]]")
	assert.Contains(t, string(updated), "[FA-0001](../../feature-areas/organization-and-onboarding-access.md)")
}

func TestRunLinkHygieneBailsOutOnAmbiguousWikilinkRewrite(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"a/target.md": "# Target A\n",
		"b/target.md": "# Target B\n",
		"source.md":   "ambiguous: [[target.md]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)

	result := RunLinkHygiene(runCtx)

	require.Equal(t, 1, result.IssueCount)
	assert.Equal(t, "wikilink_target_has_md_extension", result.Issues[0].Code)
	assert.Empty(t, result.Fixes)
}

func TestRunLinkHygieneScopesDuplicateBasenameWhenPathIsExplicit(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"a/target.md": "# Target A\n",
		"b/target.md": "# Target B\n",
		"source.md":   "explicit: [[a/target.md|A]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)

	result := RunLinkHygiene(runCtx)

	require.Equal(t, 1, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	applyCheckFixThroughPlan(t, runCtx, result, result.Fixes[0])

	updated, err := os.ReadFile(filepath.Join(root, "source.md"))
	require.NoError(t, err)
	assert.Contains(t, string(updated), "[[a/target|A]]")
}

func TestRunLinkHygieneRewritesAliasTargetsToObsidianDisplayLinks(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"docs/specs/product/search.md": `---
aliases:
  - SPEC-0001
---
# Search
`,
		"docs/efforts/effort.md": "spec: [[SPEC-0001]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)

	result := RunLinkHygiene(runCtx)

	require.Equal(t, 1, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	assert.Equal(t, "wikilink_target_is_alias", result.Issues[0].Code)
	applyCheckFixThroughPlan(t, runCtx, result, result.Fixes[0])

	updated, err := os.ReadFile(filepath.Join(root, "docs/efforts/effort.md"))
	require.NoError(t, err)
	assert.Contains(t, string(updated), "spec: [[search|SPEC-0001]]")
}

func TestRunLinkHygieneRewritesAliasTargetsWithPathWhenBasenameAmbiguous(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"docs/specs/product/search.md": `---
aliases:
  - SPEC-0001
---
# Search
`,
		"docs/specs/technical/search.md": "# Search\n",
		"docs/efforts/effort.md":         "spec: [[SPEC-0001#^story|Story]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)

	result := RunLinkHygiene(runCtx)

	require.Equal(t, 1, result.IssueCount)
	require.Len(t, result.Fixes, 1)
	assert.Equal(t, "wikilink_target_is_alias", result.Issues[0].Code)
	applyCheckFixThroughPlan(t, runCtx, result, result.Fixes[0])

	updated, err := os.ReadFile(filepath.Join(root, "docs/efforts/effort.md"))
	require.NoError(t, err)
	assert.Contains(t, string(updated), "spec: [[docs/specs/product/search#^story|Story]]")
}

func TestRunLinkHygieneDoesNotCanonicalizeAmbiguousAlias(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"docs/specs/product/search.md": `---
aliases:
  - SPEC-0001
---
# Search
`,
		"docs/specs/technical/search.md": `---
aliases:
  - SPEC-0001
---
# Search
`,
		"docs/efforts/effort.md": "spec: [[SPEC-0001]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)

	result := RunLinkHygiene(runCtx)

	require.Equal(t, 0, result.IssueCount)
	assert.Empty(t, result.Fixes)
}

func TestRunLinkHygieneReportsUnfixableInvalidForms(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"source.md": "bad abs: [[/Users/me/vault/foo]]\nplaceholder: <spec-file#^SPEC-0001-US1>\npseudo: [ [SPEC-0001] ]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)

	result := RunLinkHygiene(runCtx)

	require.Equal(t, 3, result.IssueCount)
	codes := issueCodes(result.Issues)
	assert.Contains(t, codes, "wikilink_target_absolute_path")
	assert.Contains(t, codes, "pseudo_link_placeholder")
	assert.Empty(t, result.Fixes)
}

func linkHygieneRunContext(t *testing.T, root string) RunContext {
	t.Helper()
	return RunContext{
		VaultDef:     obsidian.VaultDefinition{Path: root},
		VaultPath:    root,
		NoteReader:   &obsidian.Note{},
		NoteMetadata: testNoteMetadata(t),
		MaxIssues:    20,
	}
}

func issueCodes(issues []Issue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		out = append(out, issue.Code)
	}
	return out
}

// A dot in a note name is not an extension, and a real note or attachment
// always wins over an alias: the alias fix must never retarget a valid link.
func TestRunLinkHygieneNeverRetargetsLinksThatResolveToRealFiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"Notes/Fetch AI.md":                          "---\naliases:\n  - Fetch\n---\n# Fetch AI\n",
		"Notes/Public Ladders Career Matrix.md":      "---\naliases:\n  - Public Ladders\n---\n# Matrix\n",
		"Log/fetch.ai call with Mark, 2025-01-20.md": "# Call\n",
		"_Attachments/Public Ladders.xlsx":           "binary",
		"Notes/History.md": "- [[fetch.ai call with Mark, 2025-01-20]]\n" +
			"- [[Public Ladders.xlsx]]\n" +
			"- `![[embed]]`s in inline code\n" +
			"- [[Fetch]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)

	result := RunLinkHygiene(runCtx)

	require.Equal(t, 1, result.IssueCount, "only the genuine alias link is a hygiene issue: %+v", result.Issues)
	assert.Equal(t, "wikilink_target_is_alias", result.Issues[0].Code)
	assert.Equal(t, "Fetch", result.Issues[0].Target)
	require.Len(t, result.Fixes, 1)
	applyCheckFixThroughPlan(t, runCtx, result, result.Fixes[0])
	updated, err := os.ReadFile(filepath.Join(root, "Notes/History.md"))
	require.NoError(t, err)
	assert.Equal(t, "- [[fetch.ai call with Mark, 2025-01-20]]\n"+
		"- [[Public Ladders.xlsx]]\n"+
		"- `![[embed]]`s in inline code\n"+
		"- [[Fetch AI|Fetch]]\n", string(updated))
}

func TestRunLinkHygieneIgnoresMarkdownLinksInCode(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"docs/spec.md":  "# Spec\n",
		"docs/guide.md": "Write `[Spec](/docs/spec.md)` like this.\n\nExample:\n\n    [Spec](/docs/spec.md)\n",
	}))

	result := RunLinkHygiene(linkHygieneRunContext(t, root))

	require.Zero(t, result.IssueCount, "%+v", result.Issues)
	require.Empty(t, result.Fixes)
}
