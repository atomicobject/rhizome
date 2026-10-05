//go:build integration
// +build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

var (
	cliBuildOnce sync.Once
	cliBinary    string
	cliBuildErr  error
)

func TestPythonFixture_OntologyQuerySchemaSnapshot(t *testing.T) {
	ws := fixture.NewWorkspace(t)
	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot, "ontology", "query-schema")
	require.NoError(t, err, stderr)
	assertGoldenFile(t, "ontology_query_schema.golden.graphql", stdout)
	require.Empty(t, stderr)
}

func TestPythonFixture_OntologyReferenceSnapshot(t *testing.T) {
	ws := fixture.NewWorkspace(t)
	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot, "ontology", "reference", "--type", "Spec")
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)
	assertGoldenFile(t, "ontology_reference_spec.golden.md", stdout)
}

func TestPythonFixture_OntologyAuthoringGuideSnapshot(t *testing.T) {
	ws := fixture.NewWorkspace(t)
	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot, "ontology", "authoring-guide", "Spec")
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)
	assertGoldenFile(t, "ontology_authoring_guide_spec.golden.md", stdout)
}

func TestPythonFixture_OntologyValidateCommand(t *testing.T) {
	ws := fixture.NewWorkspace(t)
	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot, "ontology", "validate")
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Ontology valid.")
	require.Contains(t, stdout, "Resolved notes by type:")
	require.Contains(t, stdout, "Conversation (1)")
	require.Contains(t, stdout, "Decision (3)")
	require.Contains(t, stdout, "Project (1)")
	require.Contains(t, stdout, "Spec (1)")
	require.Contains(t, stdout, "Team (1)")
	require.Contains(t, stdout, "Person (2)")
}

func TestPythonFixture_QueryRecipeRunsItemBackedActionItems(t *testing.T) {
	ws := fixture.NewWorkspace(t)
	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot, "agent", "query-recipe", "run", "--id", "fixture-action-items")
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)

	var payload struct {
		Recipe struct {
			ID string `json:"id"`
		} `json:"recipe"`
		Result struct {
			Errors []any          `json:"errors"`
			Data   map[string]any `json:"data"`
		} `json:"result"`
	}
	jsonStart := strings.Index(stdout, `{"recipe"`)
	require.NotEqual(t, -1, jsonStart, stdout)
	require.NoError(t, json.NewDecoder(strings.NewReader(stdout[jsonStart:])).Decode(&payload))
	require.Equal(t, "fixture-action-items", payload.Recipe.ID)
	require.Empty(t, payload.Result.Errors)

	actionItems := payload.Result.Data["actionItem"].([]any)
	require.Len(t, actionItems, 4)
	var aliceOpen, bobCompleted bool
	for _, item := range actionItems {
		record := item.(map[string]any)
		assignee := record["assignee"].(map[string]any)
		if assignee["path"] == "notes/people/alice.md" && record["title"] == "Review search rollout notes" {
			aliceOpen = record["done"] == false && record["due"] == "2026-05-08"
		}
		if assignee["path"] == "notes/people/bob.md" && strings.Contains(record["title"].(string), "Confirm query regression") {
			bobCompleted = record["done"] == true
		}
	}
	require.True(t, aliceOpen)
	require.True(t, bobCompleted)
}

func TestPythonFixture_QueryRecipeRunsMyActionItems(t *testing.T) {
	ws := fixture.NewWorkspace(t)
	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot, "agent", "query-recipe", "run", "--id", "fixture-my-action-items", "--input", "assignee=notes/people/alice")
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)

	var payload struct {
		Recipe struct {
			ID string `json:"id"`
		} `json:"recipe"`
		Result struct {
			Errors []any          `json:"errors"`
			Data   map[string]any `json:"data"`
		} `json:"result"`
	}
	jsonStart := strings.Index(stdout, `{"recipe"`)
	require.NotEqual(t, -1, jsonStart, stdout)
	require.NoError(t, json.NewDecoder(strings.NewReader(stdout[jsonStart:])).Decode(&payload))
	require.Equal(t, "fixture-my-action-items", payload.Recipe.ID)
	require.Empty(t, payload.Result.Errors)

	actionItems := payload.Result.Data["actionItem"].([]any)
	require.Len(t, actionItems, 2)
	require.Equal(t, "Review search rollout notes", actionItems[0].(map[string]any)["title"])
	require.Equal(t, "Update release checklist", actionItems[1].(map[string]any)["title"])
}

func TestPythonFixture_OntologyValidateCommandReportsNestedSectionFailure(t *testing.T) {
	ws := fixture.NewWorkspace(t)

	specPath := filepath.Join(ws.CodeRoot, "notes", "specs", "search-rewrite.md")
	require.NoError(t, os.WriteFile(specPath, []byte(`---
type: Spec
name: Search Rewrite
---

## Requirements

Keep ontology-backed query results stable while the search pipeline changes.
`), 0o644))

	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot, "ontology", "validate")
	require.Error(t, err)
	require.Contains(t, stdout, "missing_required_section")
	require.Contains(t, stdout, "field=requirements.details")
	require.Contains(t, stderr, "ontology validation failed with 1 issue(s)")
}

func TestPythonFixture_OntologyInspectCommand(t *testing.T) {
	ws := fixture.NewWorkspace(t)

	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot, "ontology", "inspect", "notes/specs/search-rewrite.md")
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)

	var payload ontology.InspectResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.True(t, payload.OntologyAvailable)
	require.Len(t, payload.Notes, 1)
	require.Equal(t, "notes/specs/search-rewrite.md", payload.Notes[0].Path)
	require.Equal(t, "Spec", payload.Notes[0].ResolvedType)
	require.NotNil(t, payload.Notes[0].TypeDoc)
	require.Equal(t, "Spec", payload.Notes[0].TypeDoc.Name)
	require.NotNil(t, payload.Notes[0].Assessment)
}

func TestPythonFixture_OntologyInspectCommandReportsNestedSectionFailure(t *testing.T) {
	ws := fixture.NewWorkspace(t)

	specPath := filepath.Join(ws.CodeRoot, "notes", "specs", "search-rewrite.md")
	require.NoError(t, os.WriteFile(specPath, []byte(`---
type: Spec
name: Search Rewrite
---

## Requirements

Keep ontology-backed query results stable while the search pipeline changes.
`), 0o644))

	stdout, stderr, err := runOntologyCLI(t, ws.CodeRoot, "ontology", "inspect", "notes/specs/search-rewrite.md")
	require.NoError(t, err, stderr)
	require.Empty(t, stderr)

	var payload ontology.InspectResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload))
	require.Len(t, payload.Notes, 1)
	require.NotNil(t, payload.Notes[0].Assessment)

	var found bool
	for _, issue := range payload.Notes[0].Assessment.Issues {
		if issue.Code == "missing_required_section" && issue.FieldName == "requirements.details" {
			found = true
			break
		}
	}
	require.True(t, found, "expected nested section issue in inspect payload, got %#v", payload.Notes[0].Assessment.Issues)
}

func runOntologyCLI(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(cliPath(t), args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return normalizeNewlines(stdout.String()), normalizeNewlines(stderr.String()), err
}

func cliPath(t *testing.T) string {
	t.Helper()
	cliBuildOnce.Do(func() {
		tmpDir, err := os.MkdirTemp("", "rzm-integration-*")
		if err != nil {
			cliBuildErr = err
			return
		}
		cliBinary = filepath.Join(tmpDir, cliBinaryName())
		cmd := exec.Command("go", "build", "-mod=vendor", "-tags", "fts5", "-o", cliBinary, ".")
		cmd.Dir = fixture.RepoRoot(t)
		if output, err := cmd.CombinedOutput(); err != nil {
			cliBuildErr = err
			cliBuildErr = &buildError{err: err, output: normalizeNewlines(string(output))}
		}
	})
	require.NoError(t, cliBuildErr)
	return cliBinary
}

func cliBinaryName() string {
	if runtime.GOOS == "windows" {
		return "rzm.exe"
	}
	return "rzm"
}

type buildError struct {
	err    error
	output string
}

func (e *buildError) Error() string {
	return fmt.Sprintf("%v\n%s", e.err, e.output)
}

func assertGoldenFile(t *testing.T, name string, got string) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join(goldenDir(t), name))
	require.NoError(t, err)
	require.Equal(t, normalizeGolden(string(want)), normalizeGolden(got))
}

func goldenDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "testdata")
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func normalizeGolden(s string) string {
	return strings.TrimRight(normalizeNewlines(s), "\n")
}
