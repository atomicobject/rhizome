package ontology

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestSpecDrivenSchema_ParsesCanonicalProductStoryCriteria(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologyNote(t, root, "docs/specs/product/search.md", `---
type: ProductSpec
summary: Product search contract.
id: SPEC-9000
spec-status: active
last-updated: 2026-06-01
aliases:
  - SPEC-9000
---

# Search Product Spec

## Summary

Users need search results that combine code and notes.

## Goals

- Find relevant work quickly.

## Non-Goals

- Replace IDE navigation.

## User Stories

### US1 - Unified search results for task kickoff

- id:: ^SPEC-9000-US1
- summary:: Search code and notes together before opening files.
- status:: ready

#### Acceptance Criteria

- **Mixed result handles**: Search returns code anchors and note sections with stable handles.
  Scenario: code and notes both match
  Given a task mentions a known code symbol
  When search runs
  Then code anchors and note sections both appear
  verification:: go test ./pkg/search

  > [!example]- Gherkin
  > Scenario: docs rank with code
  > Given a matching reference doc
  > When search results are merged
  > Then the doc section keeps provenance

  - Given nested context
  - Then nested bullets remain criterion detail

- Results stay compact and support continuation when truncated.
  Result payloads remain short enough for callers to continue.

## Requirements

- Search MUST preserve stable result handles.
`)

	schema, err := LoadSchema(specDrivenRepoRoot())
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, validationIssuesForPath(result.ValidationIssues, "docs/specs/product/search.md"))

	spec := requireOntologyNode(t, result.Nodes, "ProductSpec", "search")
	story := requireOntologyNode(t, result.Nodes, "UserStory", "US1 - Unified search results for task kickoff")
	require.Equal(t, spec.NodeID, story.ParentNodeID)
	storyFields := fieldValuesForNode(result, story.NodeID)
	require.Equal(t, []string{"SPEC-9000-US1"}, storyFields["id"])
	require.Equal(t, []string{"Search code and notes together before opening files."}, storyFields["summary"])
	require.Equal(t, []string{"ready"}, storyFields["status"])

	criteria := requireOntologyNodes(t, result.Nodes, "AcceptanceCriterion")
	require.Len(t, criteria, 2)
	require.Equal(t, story.NodeID, criteria[0].ParentNodeID)
	require.Equal(t, "Mixed result handles", criteria[0].Title)
	require.Equal(t, "Results stay compact and support continuation when truncated.", criteria[1].Title)

	firstCriterionFields := fieldValuesForNode(result, criteria[0].NodeID)
	require.Equal(t, []string{"go test ./pkg/search"}, firstCriterionFields["verification"])

	secondCriterionFields := fieldValuesForNode(result, criteria[1].NodeID)
	require.Empty(t, secondCriterionFields["verification"])
}

func TestSpecDrivenSchema_ParsesRequirementsOnlyTechnicalSpec(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologyNote(t, root, "docs/specs/technical/indexing.md", `---
type: TechnicalSpec
summary: Technical indexing contract.
id: SPEC-9001
spec-status: active
aliases:
  - SPEC-9001
---

# Indexing Contract

## Summary

Indexing must remain deterministic.

## Goals

- Preserve deterministic rebuilds.

## Non-Goals

- Redesign storage.

## Requirements

- Indexing MUST preserve canonical node ids.
`)

	schema, err := LoadSchema(specDrivenRepoRoot())
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, validationIssuesForPath(result.ValidationIssues, "docs/specs/technical/indexing.md"))

	spec := requireOntologyNode(t, result.Nodes, "TechnicalSpec", "indexing")
	fields := fieldValuesForNode(result, spec.NodeID)
	require.Equal(t, []string{"SPEC-9001"}, fields["id"])
	require.Equal(t, []string{"active"}, fields["specstatus"])
}

func TestSpecDrivenSchema_ParsesEffortLifecycleSurface(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologyNote(t, root, "docs/efforts/search.md", `---
type: EffortNote
id: EFF-9000
name: Search effort
created-at: 2026-06-01T12:00:00Z
plan-approved-by: Colthorp
status: planned
summary: Execute the search story.
aliases:
  - EFF-9000
---

# Search Effort

## Scope

Deliver search kickoff.

## Spec Set (Frozen)

- [[docs/specs/product/search|SPEC-9000]]

## Stories In Scope (Frozen)

- [[docs/specs/product/search#^SPEC-9000-US1|SPEC-9000.US1]]

## Spec Coverage Checklist

- [ ] Story selected.

## Plan

- Implement search.

## Original Intended Delivery

Search works.

## Actual Delivered

Not delivered yet.

## Execution Notes

- 2026-06-01T12:00Z [decision] Started.

## Deviations

- None.

## Closure Checklist

- [ ] Tests pass.

## Compounding Follow-ups

- None.

## Status

Planned.
`)

	schema, err := LoadSchema(specDrivenRepoRoot())
	require.NoError(t, err)
	result, err := buildIndexFromCanonicalSourcesForTest(t, context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, "notes-hash")
	require.NoError(t, err)
	require.Empty(t, validationIssuesForPath(result.ValidationIssues, "docs/efforts/search.md"))

	effort := requireOntologyNode(t, result.Nodes, "EffortNote", "search")
	fields := fieldValuesForNode(result, effort.NodeID)
	require.Equal(t, []string{"EFF-9000"}, fields["id"])
	require.Equal(t, []string{"Search effort"}, fields["name"])
	require.Equal(t, []string{"planned"}, fields["status"])
	require.Equal(t, []string{"Execute the search story."}, fields["summary"])
	require.Equal(t, []string{"Colthorp"}, fields["planapprovedby"])
}

func specDrivenRepoRoot() string {
	return filepath.Clean(filepath.Join("..", ".."))
}

func fieldValuesForNode(result *BuildResult, nodeID string) map[string][]string {
	out := map[string][]string{}
	for _, row := range result.NodeFieldValues {
		if row.NodeID != nodeID {
			continue
		}
		out[row.FieldName] = append(out[row.FieldName], row.ValueText)
	}
	return out
}

func validationIssuesForPath(issues []ValidationIssue, notePath string) []ValidationIssue {
	out := make([]ValidationIssue, 0)
	for _, issue := range issues {
		if issue.NotePath == notePath {
			out = append(out, issue)
		}
	}
	return out
}
