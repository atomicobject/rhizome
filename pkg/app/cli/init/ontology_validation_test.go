package init

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestAgenticEngineeringOntologyUsesCanonicalTemplateAndCompanionPaths(t *testing.T) {
	path := filepath.Join("templates", "starters", templateAgenticEngineering, "rhizome", "ontology", "spec-driven.graphql")
	content, err := os.ReadFile(path)
	require.NoError(t, err)

	text := string(content)
	require.NotContains(t, text, "templates/starters/spec-driven/")
	require.NotContains(t, text, "docs/specs/process/")
	require.Contains(t, text, "templates/starters/agentic-engineering/")
	require.Contains(t, text, "docs/engineering/review-and-approval.md")
	require.NotContains(t, text, "docs/engineering/workflow.md")
	require.NotContains(t, text, "docs/engineering/efforts.md")
}

func TestStarterOntologySchemasValidateAuthoredShapes(t *testing.T) {
	tests := []struct {
		name       string
		templates  []string
		writeNotes func(t *testing.T, root string)
		validType  string
		validTitle string
		validPath  string
		invalid    string
		issueCodes []string
	}{
		{
			name:      "core",
			templates: []string{"core"},
			writeNotes: func(t *testing.T, root string) {
				writeTemplateNote(t, root, "people/alice.md", `---
display-name: Alice Example
aliases:
  - Alice
---
`)
				writeTemplateNote(t, root, "people/bob.md", `---
aliases:
  - Bob
---
`)
			},
			validType:  "Person",
			validTitle: "alice",
			validPath:  "people/alice.md",
			invalid:    "people/bob.md",
			issueCodes: []string{"missing_required_field"},
		},
		{
			name:      "action-items",
			templates: []string{"core", "action-items"},
			writeNotes: func(t *testing.T, root string) {
				writeTemplateNote(t, root, "people/alice.md", `---
display-name: Alice Example
aliases:
  - Alice
---
`)
				writeTemplateNote(t, root, "notes/good.md", `# Good

- [ ] Follow up #action-item assignee:: [[people/alice]] due:: 2026-06-02
`)
				writeTemplateNote(t, root, "notes/bad.md", `# Bad

- [ ] Follow up #action-item assignee:: [[people/alice]] due:: someday
`)
			},
			validType:  "ActionItem",
			validTitle: "Follow up",
			validPath:  "notes/good.md",
			invalid:    "notes/bad.md",
			issueCodes: []string{"field_type_mismatch"},
		},
		{
			name:      "spec-driven",
			templates: []string{templateAgenticEngineering},
			writeNotes: func(t *testing.T, root string) {
				writeTemplateNote(t, root, "docs/specs/product/good.md", `---
type: ProductSpec
summary: Product spec summary
id: SPEC-1000
spec-status: active
---

## Summary

Summary body.

## Goals

Goals body.

## Non-Goals

Non-goals body.

## User Stories

### US1 - Browse typed notes

- id:: ^SPEC-1000-US1
- summary:: Start from note families instead of guessing from the tree.
- status:: ready

#### Acceptance Criteria

- Search returns typed note families.

## Requirements

Requirements body.
`)
				writeTemplateNote(t, root, "docs/specs/product/bad.md", `---
type: ProductSpec
summary: Product spec summary
id: SPEC-1001
spec-status: active
---

## Summary

Summary body.

## Goals

Goals body.

## Non-Goals

Non-goals body.

## User Stories

### As an implementer starting a task

id:: ^SPEC-1001-US1
summary:: Start from note families instead of guessing from the tree.
status:: ready

#### Acceptance Criteria

## Requirements

Requirements body.
`)
			},
			validType:  "UserStory",
			validTitle: "US1 - Browse typed notes",
			validPath:  "docs/specs/product/good.md",
			invalid:    "docs/specs/product/bad.md",
			issueCodes: []string{"title_forbidden_pattern", "field_authoring_style_mismatch", "contains_min_not_met"},
		},
		{
			name:      "complex-domain",
			templates: []string{templateAgenticEngineering, "complex-domain"},
			writeNotes: func(t *testing.T, root string) {
				writeTemplateNote(t, root, "docs/reference/requirements/requirements/good.md", `---
id: REQ-0001
summary: Good requirement
status: accepted
kind: capability
confidence: source_backed
---
`)
				writeTemplateNote(t, root, "docs/reference/requirements/requirements/bad.md", `---
id: REQ-0002
summary: Bad requirement
status: superseded
kind: capability
confidence: source_backed
---
`)
			},
			validType:  "Requirement",
			validTitle: "good",
			validPath:  "docs/reference/requirements/requirements/good.md",
			invalid:    "docs/reference/requirements/requirements/bad.md",
			issueCodes: []string{"conditional_required_field_missing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			installStarterOntologySchemas(t, root, tt.templates...)
			tt.writeNotes(t, root)

			schema, err := ontology.LoadSchema(root)
			require.NoError(t, err)
			sources, err := testNoteMetadataIndexer(t).BuildNoteSourceSnapshots(context.Background(), obsidian.VaultDefinition{Path: root}, &obsidian.Note{})
			require.NoError(t, err)
			result, err := ontology.BuildIndexFromNoteSources(context.Background(), obsidian.VaultDefinition{Path: root}, sources, schema, "notes-hash")
			require.NoError(t, err)
			requireStarterNode(t, result.Nodes, tt.validType, tt.validTitle)
			require.Empty(t, starterIssuesForPath(result.ValidationIssues, tt.validPath, ""))

			invalidIssues := starterIssuesForPath(result.ValidationIssues, tt.invalid, "")
			for _, code := range tt.issueCodes {
				requireStarterIssueCode(t, invalidIssues, code)
			}
		})
	}
}

func installStarterOntologySchemas(t *testing.T, root string, templates ...string) {
	t.Helper()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, template := range templates {
		files, err := loadStarterOntologyTemplates(template)
		require.NoError(t, err)
		require.NotEmpty(t, files, "starter %s should have ontology files", template)
		for _, file := range files {
			require.NoError(t, os.WriteFile(filepath.Join(dir, file.Path), file.Content, 0o644))
		}
	}
}

func writeTemplateNote(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

func requireStarterNode(t *testing.T, nodes []codeanchor.IntelOntologyNode, typeName, title string) {
	t.Helper()
	for _, node := range nodes {
		if node.TypeName == typeName && node.Title == title {
			return
		}
	}
	require.Failf(t, "missing ontology node", "type=%s title=%s", typeName, title)
}

func starterIssuesForPath(issues []ontology.ValidationIssue, notePath string, code string) []ontology.ValidationIssue {
	out := make([]ontology.ValidationIssue, 0)
	for _, issue := range issues {
		if notePath != "" && issue.NotePath != notePath {
			continue
		}
		if code != "" && issue.Code != code {
			continue
		}
		out = append(out, issue)
	}
	return out
}

func requireStarterIssueCode(t *testing.T, issues []ontology.ValidationIssue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	require.Failf(t, "missing issue code", "code=%s issues=%+v", code, issues)
}
