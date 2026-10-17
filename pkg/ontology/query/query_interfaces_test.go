package query

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecute_ProjectKBStarterPatternResolvesStructuralRelations(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
enum ArtifactStatus {
  DRAFT
  ACTIVE
}

enum QuestionStatus {
  OPEN
  ANSWERED
}

"""
Project operating hub.
Create one when the repo needs a durable center of gravity for decisions, artifacts, and open questions.
"""
type Project @node(paths: ["notes/projects/*.md"]) {
  """Project summary."""
  summary: String!
  decisions: [Decision!] @link(inverse: "project")
  artifacts: [Artifact!] @link(inverse: "project")
  openQuestions: [OpenQuestion!] @link(inverse: "project")
}

"""Decision note."""
type Decision @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
  project: Project! @link(inverse: "decisions")
}

"""Artifact note."""
type Artifact @node(paths: ["notes/artifacts/*.md"]) {
  summary: String!
  status: ArtifactStatus!
  project: Project! @link(inverse: "artifacts")
}

"""Open question note."""
type OpenQuestion @node(paths: ["notes/questions/*.md"]) {
  summary: String!
  status: QuestionStatus!
  project: Project! @link(inverse: "openQuestions")
}
`, map[string]string{
		"notes/projects/atlas.md": `---
type: Project
summary: Atlas operating hub
decisions:
  - notes/decisions/scope-freeze.md
artifacts:
  - notes/artifacts/brief.md
open-questions:
  - notes/questions/api-shape.md
---
`,
		"notes/decisions/scope-freeze.md": `---
type: Decision
summary: Freeze scope for v1
project: notes/projects/atlas.md
---
`,
		"notes/artifacts/brief.md": `---
type: Artifact
summary: Client brief
status: ACTIVE
project: notes/projects/atlas.md
---
`,
		"notes/questions/api-shape.md": `---
type: OpenQuestion
summary: Finalize public API shape
status: OPEN
project: notes/projects/atlas.md
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `
{
  project(path: "notes/projects/atlas.md") {
    summary
    decisions { title }
    artifacts { title }
    openQuestions { title }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, "Atlas operating hub", record["summary"])
	require.Equal(t, []string{"scope-freeze"}, titles(record["decisions"].([]any)))
	require.Equal(t, []string{"brief"}, titles(record["artifacts"].([]any)))
	require.Equal(t, []string{"api-shape"}, titles(record["openQuestions"].([]any)))
}

func TestExecute_ResolvesInterfacesAndSections(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type RequirementsSection implements Section {
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision", scope: SUBTREE)
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
  requirements: RequirementsSection @contains(level: H2, heading: "Requirements", required: true)
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---

# Intro

Context.

## Requirements

See [[decision-one]].

### Details

Extra detail.
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: Decision summary
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `
{
  notes(type: "SummaryDoc", find: "notes") {
    nodes {
      ... on SummaryDoc { summary }
      ... on Spec {
        requirements {
          title
          level
          content
          children { title }
          decisions { title }
        }
      }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["notes"].(map[string]any)["nodes"].([]any)
	require.Len(t, rows, 2)

	var spec map[string]any
	for _, row := range rows {
		record := row.(map[string]any)
		if _, ok := record["requirements"]; ok {
			spec = record
			break
		}
	}
	require.NotNil(t, spec)
	require.Equal(t, "Spec summary", spec["summary"])

	requirements := spec["requirements"].(map[string]any)
	require.Equal(t, "Requirements", requirements["title"])
	require.Equal(t, "H2", requirements["level"])
	require.Contains(t, requirements["content"], "See [[decision-one]]")
	require.Equal(t, []string{"Details"}, titles(requirements["children"].([]any)))
	require.Equal(t, []string{"decision-one"}, titles(requirements["decisions"].([]any)))
}

func TestExecute_ResolvesBareInterfaceTypeFilter(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Spec implements SummaryDoc @node(paths: ["notes/specs/*.md"]) {
  summary: String!
}
`, map[string]string{
		"notes/specs/spec.md": `---
type: Spec
summary: Spec summary
---
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: Decision summary
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `
{
  notes(type: "SummaryDoc") {
    nodes {
      title
      ... on SummaryDoc { summary }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["notes"].(map[string]any)["nodes"].([]any)
	require.Len(t, rows, 2)
	require.Equal(t, []string{"decision-one", "spec"}, titles(rows))
}

func TestExecute_AmbientBuiltinsApplyFirstAfterInterfaceFiltering(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface SummaryDoc {
  summary: String!
}

type Alert @node(paths: ["notes/alerts/*.md"]) {
  summary: String!
}

type Decision implements SummaryDoc @node(paths: ["notes/decisions/*.md"]) {
  summary: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  summary: String!
}
`, map[string]string{
		"notes/projects/roadmap.md": `---
type: Project
summary: Roadmap
---

See [[alert-one]] and [[decision-one]].
`,
		"notes/alerts/alert-one.md": `---
type: Alert
summary: Alert summary
---
`,
		"notes/decisions/decision-one.md": `---
type: Decision
summary: Decision summary
---
`,
	})

	prepared, errs := Prepare(env.execSchema, `{ project(path: "notes/projects/roadmap.md") { linked(type: "SummaryDoc", first: 1) { title } } }`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	record := rows[0].(map[string]any)
	require.Equal(t, []string{"decision-one"}, titles(record["linked"].([]any)))
}
