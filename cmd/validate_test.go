package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestValidateCommandBrokenLinksPasses(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"A.md": "# A\n\n[[B]]\n",
		"B.md": "# B\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"validate", "broken-links", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "[completed] broken-links")
	require.Contains(t, stdout, "Validation summary: 0 issues; 0 errors; exit=0")
}

func TestValidateCommandBrokenLinksFails(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"A.md": "# A\n\n[[Missing]]\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"validate", "broken-links", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "[completed] broken-links")
	require.Contains(t, stdout, "Validation summary:")
	require.Contains(t, stdout, "Repair plan:")
	require.Contains(t, stdout, "agent-required")
	require.Contains(t, stdout, "rzm agent semantic-query --query Missing")
}

func TestAgentValidateCompanionDocsPasses(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/config.yml": "notes: {}\ncode:\n  enabled: true\n",
		".rhizome/ontology/schema.graphql": `
type Project @node(paths: ["docs/projects/*.md"]) @companionDocs(paths: ["docs/reference/project-workflow.md"], purpose: "workflow") {
  name: String!
}
`,
		"docs/reference/project-workflow.md": "# Project workflow\n",
		"docs/projects/alpha.md": `---
name: Alpha
---

# Alpha
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "companion-docs", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)

	var resp struct {
		OK     bool `json:"ok"`
		Checks []struct {
			Name       string `json:"name"`
			OK         bool   `json:"ok"`
			IssueCount int    `json:"issueCount"`
			Summary    string `json:"summary"`
		} `json:"checks"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.True(t, resp.OK, stdout)
	var check struct {
		Name       string `json:"name"`
		OK         bool   `json:"ok"`
		IssueCount int    `json:"issueCount"`
		Summary    string `json:"summary"`
	}
	for _, candidate := range resp.Checks {
		if candidate.Name == "companion-docs" {
			check = candidate
			break
		}
	}
	require.Equal(t, "companion-docs", check.Name)
	require.True(t, check.OK)
	require.Equal(t, 0, check.IssueCount)
	require.Contains(t, check.Summary, "companion doc path")
}

func TestAgentValidateCompanionDocsFlagsMissingPath(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/config.yml": "notes: {}\ncode:\n  enabled: true\n",
		".rhizome/ontology/schema.graphql": `
type Project @node(paths: ["docs/projects/*.md"]) @companionDocs(paths: ["docs/reference/missing.md"], purpose: "workflow") {
  name: String!
}
`,
		"docs/projects/alpha.md": `---
name: Alpha
---

# Alpha
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "companion-docs", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)

	var resp struct {
		OK     bool `json:"ok"`
		Checks []struct {
			Name       string `json:"name"`
			OK         bool   `json:"ok"`
			IssueCount int    `json:"issueCount"`
			Issues     []struct {
				Code    string `json:"code"`
				Path    string `json:"path"`
				Source  string `json:"source"`
				Target  string `json:"target"`
				Message string `json:"message"`
			} `json:"issues"`
		} `json:"checks"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.False(t, resp.OK)
	var check struct {
		Name       string `json:"name"`
		OK         bool   `json:"ok"`
		IssueCount int    `json:"issueCount"`
		Issues     []struct {
			Code    string `json:"code"`
			Path    string `json:"path"`
			Source  string `json:"source"`
			Target  string `json:"target"`
			Message string `json:"message"`
		} `json:"issues"`
	}
	for _, candidate := range resp.Checks {
		if candidate.Name == "companion-docs" {
			check = candidate
			break
		}
	}
	require.Equal(t, "companion-docs", check.Name)
	require.False(t, check.OK)
	require.Equal(t, 1, check.IssueCount)
	require.Len(t, check.Issues, 1)
	require.Equal(t, "companion_doc_missing", check.Issues[0].Code)
	require.Equal(t, "docs/reference/missing.md", check.Issues[0].Path)
	require.Equal(t, "Project", check.Issues[0].Source)
	require.Equal(t, "docs/reference/missing.md", check.Issues[0].Target)
}

// aliasTestSchema declares a Spec note type with a preferred identifier field
// so the aliases validator has something to check. The propertyCase directive
// pins the frontmatter property key to the literal field name ("specId") so
// the test frontmatter matches what the schema expects.
const aliasTestSchema = `
type Spec @node(paths: ["notes/specs/*.md"], propertyCase: AS_DEFINED) {
  name: String!
  specId: String @field @identifier(preferred: true)
}
`

// aliasValidateJSON is the minimal shape we need to assert on the
// agent-validate output for the aliases check.
type aliasValidateJSON struct {
	OK         bool `json:"ok"`
	IssueCount int  `json:"issueCount"`
	Outcomes   []struct {
		Check   string `json:"check"`
		Outcome string `json:"outcome"`
		Summary string `json:"summary"`
	} `json:"outcomes"`
	NextActions struct {
		SafeAutoFixCommand string `json:"safeAutoFixCommand"`
		Actions            []struct {
			Category string `json:"category"`
			Command  string `json:"command"`
		} `json:"actions"`
	} `json:"nextActions"`
	Checks []struct {
		Name       string `json:"name"`
		OK         bool   `json:"ok"`
		Skipped    bool   `json:"skipped"`
		IssueCount int    `json:"issueCount"`
		Summary    string `json:"summary"`
		Issues     []struct {
			Code    string `json:"code"`
			Path    string `json:"path"`
			Field   string `json:"field"`
			Source  string `json:"source"`
			Target  string `json:"target"`
			Message string `json:"message"`
		} `json:"issues"`
	} `json:"checks"`
}

func decodeAliasValidateOutput(t *testing.T, stdout string) aliasValidateJSON {
	t.Helper()
	var resp aliasValidateJSON
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	return resp
}

func findAliasCheckResult(t *testing.T, resp aliasValidateJSON) aliasValidateJSON {
	t.Helper()
	for _, c := range resp.Checks {
		if c.Name == "identifiers" {
			resp.Checks = []struct {
				Name       string `json:"name"`
				OK         bool   `json:"ok"`
				Skipped    bool   `json:"skipped"`
				IssueCount int    `json:"issueCount"`
				Summary    string `json:"summary"`
				Issues     []struct {
					Code    string `json:"code"`
					Path    string `json:"path"`
					Field   string `json:"field"`
					Source  string `json:"source"`
					Target  string `json:"target"`
					Message string `json:"message"`
				} `json:"issues"`
			}{c}
			return resp
		}
	}
	t.Fatalf("identifiers check not found in response: %+v", resp.Checks)
	return resp
}

// TestValidateAliasesCleanVault verifies that a spec note whose identifier is
// correctly mirrored into the aliases list passes the aliases check with no
// issues.
func TestValidateAliasesCleanVault(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": aliasTestSchema,
		"notes/specs/indexed-search.md": `---
type: Spec
name: Indexed Search
specId: SPEC-001
aliases:
  - SPEC-001
---

# SPEC-001
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "aliases", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)

	resp := decodeAliasValidateOutput(t, stdout)
	require.True(t, resp.OK, "expected ok, got: %s", stdout)
	require.Equal(t, 0, resp.IssueCount)
	resp = findAliasCheckResult(t, resp)
	require.Equal(t, 0, resp.Checks[0].IssueCount)
	require.False(t, resp.Checks[0].Skipped)
	require.Contains(t, resp.Checks[0].Summary, "identifier nodes checked")
}

// TestValidateAliasesFlagsMissingMirror verifies that a spec note that declares
// an @identifier field but does not mirror it into aliases emits the
// identifier_not_in_aliases issue.
func TestValidateAliasesFlagsMissingMirror(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": aliasTestSchema,
		"notes/specs/bad.md": `---
type: Spec
name: Bad Spec
specId: SPEC-002
---

# SPEC-002
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "aliases", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)

	resp := decodeAliasValidateOutput(t, stdout)
	require.False(t, resp.OK)
	require.GreaterOrEqual(t, resp.IssueCount, 1)
	require.Contains(t, resp.NextActions.SafeAutoFixCommand, "rzm agent validate fix aliases --apply --vault "+vault.name)
	require.NotContains(t, resp.NextActions.SafeAutoFixCommand, "--fix")
	require.NotContains(t, resp.NextActions.SafeAutoFixCommand, "--check")
	require.NotContains(t, resp.NextActions.SafeAutoFixCommand, "--non-interactive")
	require.NotEmpty(t, resp.NextActions.Actions)
	require.Equal(t, "safe_auto_fix", resp.NextActions.Actions[0].Category)
	require.Equal(t, resp.NextActions.SafeAutoFixCommand, resp.NextActions.Actions[0].Command)

	resp = findAliasCheckResult(t, resp)
	check := resp.Checks[0]
	require.Equal(t, 1, check.IssueCount)
	require.Len(t, check.Issues, 1)
	issue := check.Issues[0]
	require.Equal(t, "identifier_not_in_aliases", issue.Code)
	require.Equal(t, "notes/specs/bad.md", issue.Path)
	require.Equal(t, "specId", issue.Field)
	require.Equal(t, "SPEC-002", issue.Target)
	require.Contains(t, issue.Message, "SPEC-002")
}

// TestValidateAliasesFlagsDuplicatePreferred verifies that two spec notes
// declaring the same preferred identifier trigger the
// duplicate_preferred_identifier issue (one issue per owner path).
func TestValidateAliasesFlagsDuplicatePreferred(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": aliasTestSchema,
		"notes/specs/a.md": `---
type: Spec
name: Spec A
specId: SPEC-003
aliases:
  - SPEC-003
---

# Spec A
`,
		"notes/specs/b.md": `---
type: Spec
name: Spec B
specId: SPEC-003
aliases:
  - SPEC-003
---

# Spec B
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "aliases", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)

	resp := decodeAliasValidateOutput(t, stdout)
	require.False(t, resp.OK)
	resp = findAliasCheckResult(t, resp)
	check := resp.Checks[0]

	var (
		sawA bool
		sawB bool
	)
	require.NotEmpty(t, check.Issues)
	for _, issue := range check.Issues {
		require.Equal(t, "duplicate_preferred_identifier", issue.Code, "unexpected issue: %+v", issue)
		require.Equal(t, "spec-003", issue.Target)
		if issue.Path == "notes/specs/a.md" {
			sawA = true
		}
		if issue.Path == "notes/specs/b.md" {
			sawB = true
		}
	}
	require.True(t, sawA, "expected duplicate issue for spec A")
	require.True(t, sawB, "expected duplicate issue for spec B")
}

// TestValidateAliasesIsCaseSensitive pins plan Open Risk #1: identifier
// values are matched case-sensitively against aliases. Even though Obsidian
// is case-insensitive on macOS, identifiers are machine-typed and strict
// equality keeps duplicate detection and wikilink resolution predictable.
func TestValidateAliasesIsCaseSensitive(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": aliasTestSchema,
		"notes/specs/case.md": `---
type: Spec
name: Case Spec
specId: SPEC-100
aliases:
  - spec-100
---

# SPEC-100
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "aliases", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)

	resp := decodeAliasValidateOutput(t, stdout)
	require.False(t, resp.OK)
	resp = findAliasCheckResult(t, resp)
	require.Equal(t, 1, resp.Checks[0].IssueCount)
	require.Equal(t, "identifier_not_in_aliases", resp.Checks[0].Issues[0].Code)
}

// TestValidateAliasesSkipsWithoutIdentifier verifies the identifiers check
// reports an explicit not-applicable outcome when the ontology declares no
// @identifier fields at all.
func TestValidateAliasesSkipsWithoutIdentifier(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["notes/specs/*.md"]) {
  name: String!
}
`,
		"notes/specs/plain.md": `---
type: Spec
name: Plain Spec
---

# Plain
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "aliases", "--vault", vault.name})
	require.NoError(t, err)
	require.Empty(t, stderr)

	resp := decodeAliasValidateOutput(t, stdout)
	require.True(t, resp.OK)
	require.Empty(t, resp.Checks)
	require.Len(t, resp.Outcomes, 1)
	require.Equal(t, "identifiers", resp.Outcomes[0].Check)
	require.Equal(t, "not_applicable", resp.Outcomes[0].Outcome)
	require.Contains(t, resp.Outcomes[0].Summary, "no @identifier fields")
}

func TestAgentValidateBrokenLinksIncludesGroupedFixSuggestion(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		"A.md":            "# A\n\n[[Missing Link]]\n",
		"B.md":            "# B\n\n[[Missing Link]]\n",
		"missing-link.md": "# Candidate\n",
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "broken-links", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)

	var resp struct {
		NextActions struct {
			SafeAutoFixCommand     string `json:"safeAutoFixCommand"`
			NeedsConfirmationCount int    `json:"needsConfirmationCount"`
			ClassificationRequired bool   `json:"classificationRequired"`
			Actions                []struct {
				Category string `json:"category"`
				Message  string `json:"message"`
				Count    int    `json:"count"`
			} `json:"actions"`
		} `json:"nextActions"`
		FixPlan *struct {
			Actions []struct {
				ID             string   `json:"id"`
				Check          string   `json:"check"`
				Safety         string   `json:"safety"`
				Question       string   `json:"question"`
				InstanceCount  int      `json:"instanceCount"`
				AffectedPaths  []string `json:"affectedPaths"`
				CandidatePaths []string `json:"candidatePaths"`
			} `json:"actions"`
		} `json:"fixPlan"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.NotNil(t, resp.FixPlan)
	require.Len(t, resp.FixPlan.Actions, 1)
	action := resp.FixPlan.Actions[0]
	require.Equal(t, "broken_links", action.Check)
	require.Equal(t, string(validate.FixSafetyConfirm), action.Safety)
	require.Equal(t, 2, action.InstanceCount)
	require.ElementsMatch(t, []string{"A.md", "B.md"}, action.AffectedPaths)
	require.Equal(t, []string{"missing-link.md"}, action.CandidatePaths)
	require.Contains(t, action.Question, "[[missing-link|Missing Link]]")
	require.Equal(t, 1, resp.NextActions.NeedsConfirmationCount)
	require.False(t, resp.NextActions.ClassificationRequired)
	require.NotEmpty(t, resp.NextActions.Actions)
	require.Equal(t, "needs_confirmation", resp.NextActions.Actions[0].Category)
	require.Len(t, resp.NextActions.Actions, 1)
}

func TestAgentValidateFixNonInteractiveAppliesAliasMirror(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": aliasTestSchema,
		"notes/specs/bad.md": `---
type: Spec
name: Bad Spec
specId: SPEC-002
---

# SPEC-002
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "fix", "aliases", "--vault", vault.name, "--apply"})
	require.NoError(t, err)
	require.Empty(t, stderr)

	resp := decodeAliasValidateOutput(t, stdout)
	require.True(t, resp.OK, "expected fix run to clear alias issues: %s", stdout)
	require.Equal(t, 0, resp.IssueCount)

	updated, readErr := os.ReadFile(filepath.Join(vault.path, "notes/specs/bad.md"))
	require.NoError(t, readErr)
	require.Contains(t, string(updated), "aliases:")
	require.Contains(t, string(updated), "SPEC-002")
}

func TestAgentValidateFixNonInteractiveFillsBlankIdentifierField(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["notes/specs/*.md"], propertyCase: AS_DEFINED) {
  name: String!
  specId: String! @field @identifier(preferred: true)
}
`,
		"notes/specs/bad.md": `---
type: Spec
name: Bad Spec
specId:
aliases:
  - SPEC-003
---

# SPEC-003
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "fix", "aliases", "--vault", vault.name, "--apply"})
	require.NoError(t, err)
	require.Empty(t, stderr)

	resp := decodeAliasValidateOutput(t, stdout)
	require.True(t, resp.OK, "expected fix run to clear alias issues: %s", stdout)
	require.Equal(t, 0, resp.IssueCount)

	updated, readErr := os.ReadFile(filepath.Join(vault.path, "notes/specs/bad.md"))
	require.NoError(t, readErr)
	require.Contains(t, string(updated), "specId: SPEC-003")
}

func TestAgentValidateReportsConflictingIdentifierBackfills(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type Spec @node(paths: ["notes/specs/*.md"], propertyCase: AS_DEFINED) {
  name: String!
  specId: String! @field @identifier(preferred: true)
}
`,
		"notes/specs/a.md": `---
type: Spec
name: Spec A
aliases:
  - SPEC-010
---

# Spec A
`,
		"notes/specs/b.md": `---
type: Spec
name: Spec B
aliases:
  - SPEC-010
---

# Spec B
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "aliases", "--vault", vault.name})
	require.Error(t, err)
	require.Empty(t, stderr)

	var resp struct {
		Checks []struct {
			Issues []struct {
				Key   string `json:"issueKey"`
				Code  string `json:"code"`
				Path  string `json:"path"`
				Field string `json:"field"`
			} `json:"issues"`
		} `json:"checks"`
		FixPlan *struct {
			Actions []struct {
				ID        string   `json:"id"`
				Check     string   `json:"check"`
				IssueCode string   `json:"issueCode"`
				Kind      string   `json:"kind"`
				Safety    string   `json:"safety"`
				Title     string   `json:"title"`
				Summary   string   `json:"summary"`
				IssueKeys []string `json:"issueKeys"`
				Affected  []string `json:"affectedPaths"`
			} `json:"actions"`
		} `json:"fixPlan"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.NotNil(t, resp.FixPlan)
	require.Len(t, resp.FixPlan.Actions, 3)
	require.Equal(t, string(validate.FixSafetySafe), resp.FixPlan.Actions[0].Safety)
	require.Contains(t, resp.FixPlan.Actions[0].ID, "identifier-from-alias:")
	require.Equal(t, string(validate.FixSafetyAgent), resp.FixPlan.Actions[1].Safety)
	require.Contains(t, resp.FixPlan.Actions[1].Summary, "choose a replacement explicitly")
	require.ElementsMatch(t, []string{"notes/specs/a.md", "notes/specs/b.md"}, resp.FixPlan.Actions[1].Affected)

	var missingBKey string
	for _, check := range resp.Checks {
		for _, issue := range check.Issues {
			if issue.Code == "missing_required_field" && issue.Path == "notes/specs/b.md" && issue.Field == "specId" {
				missingBKey = issue.Key
			}
		}
	}
	require.NotEmpty(t, missingBKey)
	require.Equal(t, struct {
		ID        string   `json:"id"`
		Check     string   `json:"check"`
		IssueCode string   `json:"issueCode"`
		Kind      string   `json:"kind"`
		Safety    string   `json:"safety"`
		Title     string   `json:"title"`
		Summary   string   `json:"summary"`
		IssueKeys []string `json:"issueKeys"`
		Affected  []string `json:"affectedPaths"`
	}{
		ID:        "remediation:" + strings.TrimPrefix(missingBKey, "issue:v1:"),
		Check:     validate.CheckIdentifiers,
		IssueCode: "missing_required_field",
		Kind:      "review_missing_required_field",
		Safety:    string(validate.FixSafetyAgent),
		Title:     "Resolve missing required field",
		Summary:   "Set the exact missing identifier field from one unreserved alias when proven; otherwise review the path-derived confirmation candidate.",
		IssueKeys: []string{missingBKey},
		Affected:  []string{"notes/specs/b.md"},
	}, resp.FixPlan.Actions[2])
}

func TestAgentValidateFixNonInteractiveAddsMissingRequiredSections(t *testing.T) {
	vault := setupAgentTestVault(t, map[string]string{
		".rhizome/ontology/schema.graphql": `
type SpecSection implements Section {
  notes: String @field
}

type Spec @node(paths: ["notes/specs/*.md"], propertyCase: AS_DEFINED) {
  name: String!
  requirements: SpecSection @contains(level: H2, heading: "Requirements", required: true)
  validation: SpecSection @contains(level: H2, heading: "Validation", required: true)
}
`,
		"notes/specs/spec.md": `---
type: Spec
name: Missing Sections
---

# Missing Sections
`,
	})

	stdout, stderr, err := runRootCLI(t, nil, []string{"agent", "validate", "fix", "ontology", "--vault", vault.name, "--apply"})
	require.Error(t, err, stdout)
	require.Empty(t, stderr, stdout)

	var resp struct {
		OK         bool `json:"ok"`
		IssueCount int  `json:"issueCount"`
		Checks     []struct {
			Name   string `json:"name"`
			Issues []struct {
				Code string `json:"code"`
			} `json:"issues"`
		} `json:"checks"`
		FixExecution *struct {
			Applied []string `json:"applied"`
		} `json:"fixExecution"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resp))
	require.False(t, resp.OK, "expected empty required sections to remain invalid after scaffolding: %s", stdout)
	require.Equal(t, 2, resp.IssueCount)
	require.NotNil(t, resp.FixExecution)
	require.Len(t, resp.FixExecution.Applied, 2)
	var ontologyIssues []struct {
		Code string `json:"code"`
	}
	for _, check := range resp.Checks {
		if check.Name == "ontology" {
			ontologyIssues = check.Issues
			break
		}
	}
	require.Len(t, ontologyIssues, 2)
	for _, issue := range ontologyIssues {
		require.Equal(t, "empty_required_section", issue.Code)
	}

	updated, readErr := os.ReadFile(filepath.Join(vault.path, "notes/specs/spec.md"))
	require.NoError(t, readErr)
	require.Contains(t, string(updated), "## Requirements")
	require.Contains(t, string(updated), "## Validation")
}
