package query

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExecuteRelationCountResolvesAndBatchesAcrossParents(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  decisions: [Decision!] @neighbors(direction: OUTBOUND, type: "Decision")
}
`, map[string]string{
		"notes/projects/alpha.md": "---\nname: Alpha\n---\nSee [[one]].\n",
		"notes/projects/beta.md":  "---\nname: Beta\n---\nSee [[one]] and [[two]].\n",
		"notes/decisions/one.md":  "---\nname: One\n---\n",
		"notes/decisions/two.md":  "---\nname: Two\n---\n",
	})
	prepared, errs := Prepare(env.execSchema, `{ project(find: "Project", first: 2) { name decisionsCount } }`)
	require.Empty(t, errs)
	spy := &relationBatchSpyStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	rows := result.Data["project"].([]any)
	require.Equal(t, 1, rows[0].(map[string]any)["decisionsCount"])
	require.Equal(t, 2, rows[1].(map[string]any)["decisionsCount"])
	require.Equal(t, 1, spy.ambientCalls)
	require.Equal(t, 2, spy.maxAmbientSources)
}

func TestExecute_ResolvesStructuralAndAmbientFields(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  project(path: "notes/projects/roadmap-refresh.md") {
    path
    title
    name
    owner { title }
    decisionNotes { title }
    linked(type: "Decision") { title }
    backlinked(type: "Decision") { title }
    connected(type: "Decision") { title }
    decisions { title }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["project"].([]any)
	require.Len(t, rows, 1)
	root := rows[0].(map[string]any)
	require.Equal(t, "notes/projects/roadmap-refresh.md", root["path"])
	require.Equal(t, "Roadmap Refresh", root["name"])
	require.Equal(t, "Alice", root["owner"].(map[string]any)["title"])

	decisionNotes := titles(root["decisionNotes"].([]any))
	require.Equal(t, []string{"Structured Ontology Decision"}, decisionNotes)

	linked := titles(root["linked"].([]any))
	require.Equal(t, []string{"Ambient Linked Decision"}, linked)

	backlinked := titles(root["backlinked"].([]any))
	require.Equal(t, []string{"Backlink Decision"}, backlinked)

	connected := titles(root["connected"].([]any))
	require.Equal(t, []string{"Backlink Decision", "Ambient Linked Decision"}, connected)

	neighbors := titles(root["decisions"].([]any))
	require.Equal(t, []string{"Backlink Decision", "Ambient Linked Decision"}, neighbors)
}

func TestExecute_BatchesRelationFieldsAcrossNodeLists(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Person @node(paths: ["notes/people/*.md"]) {
  name: String!
}

type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
  owner: Person! @link
  decisions: [Decision!] @neighbors(direction: BOTH, type: "Decision")
}
`, map[string]string{
		"notes/people/alice.md": `---
type: Person
name: Alice
---
`,
		"notes/projects/alpha.md": `---
type: Project
name: Alpha Project
owner: notes/people/alice.md
---

See [[decision-alpha]].
`,
		"notes/projects/beta.md": `---
type: Project
name: Beta Project
owner: notes/people/alice.md
---

See [[decision-beta]].
`,
		"notes/decisions/decision-alpha.md": `---
type: Decision
name: Decision Alpha
---

References [[alpha]].
`,
		"notes/decisions/decision-beta.md": `---
type: Decision
name: Decision Beta
---

References [[beta]].
`,
	})
	prepared, errs := Prepare(env.execSchema, `
{
  project(find: "Project", first: 2) {
    path
    owner { path title }
    linked(type: "Decision") { path title }
    backlinked(type: "Decision") { path title }
    connected(type: "Decision") { path title }
    decisions { path title }
  }
}
`)
	require.Empty(t, errs)

	spy := &relationBatchSpyStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["project"].([]any)
	require.Len(t, rows, 2)
	require.ElementsMatch(t, []string{"notes/projects/alpha.md", "notes/projects/beta.md"}, []string{
		rows[0].(map[string]any)["path"].(string),
		rows[1].(map[string]any)["path"].(string),
	})
	for _, row := range rows {
		project := row.(map[string]any)
		path := project["path"].(string)
		decisionPath := "notes/decisions/decision-alpha.md"
		decisionTitle := "Decision Alpha"
		if path == "notes/projects/beta.md" {
			decisionPath, decisionTitle = "notes/decisions/decision-beta.md", "Decision Beta"
		} else {
			require.Equal(t, "notes/projects/alpha.md", path)
		}
		require.Equal(t, "notes/people/alice.md", project["owner"].(map[string]any)["path"])
		require.Equal(t, "Alice", project["owner"].(map[string]any)["title"])
		for _, field := range []string{"linked", "backlinked", "connected", "decisions"} {
			require.Equal(t, []any{map[string]any{"path": decisionPath, "title": decisionTitle}}, project[field], field)
		}
	}
	require.Equal(t, 1, spy.structuralCalls)
	require.Equal(t, 3, spy.ambientCalls)
	require.Equal(t, 2, spy.maxStructuralSources)
	require.Equal(t, 2, spy.maxAmbientSources)
}

func TestExecute_BatchesNestedRelationFieldsAcrossParentLists(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Decision @node(paths: ["notes/decisions/*.md"]) {
  name: String!
}

type Project @node(paths: ["notes/projects/*.md"]) {
  name: String!
}
`, map[string]string{
		"notes/projects/alpha.md": `---
type: Project
name: Alpha Project
---

See [[decision-alpha]].
`,
		"notes/projects/beta.md": `---
type: Project
name: Beta Project
---

See [[decision-beta]].
`,
		"notes/decisions/decision-alpha.md": `---
type: Decision
name: Decision Alpha
---

See [[decision-shared]].
`,
		"notes/decisions/decision-beta.md": `---
type: Decision
name: Decision Beta
---

See [[decision-shared]].
`,
		"notes/decisions/decision-shared.md": `---
type: Decision
name: Decision Shared
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `
{
  project(find: "Project", first: 2) {
    linked(type: "Decision") {
      title
      connected(type: "Decision") { title }
    }
  }
}
`)
	require.Empty(t, errs)

	spy := &relationBatchSpyStore{Store: env.store}
	deps := env.deps(nil)
	deps.Store = spy

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	rows := result.Data["project"].([]any)
	require.Len(t, rows, 2)
	firstLinked := rows[0].(map[string]any)["linked"].([]any)
	require.NotEmpty(t, firstLinked)
	require.Equal(t, []string{"Decision Shared"}, titles(firstLinked[0].(map[string]any)["connected"].([]any)))
	require.Equal(t, 2, spy.ambientCalls)
	require.Equal(t, 2, spy.maxAmbientSources)
}

func TestExecute_DedupesDuplicateAmbientRelationRows(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type concept @node(paths: ["concepts/*.md"]) {
  name: String!
  connectedConcepts: [concept!] @neighbors(direction: OUTBOUND, type: "concept")
}
`, map[string]string{
		"concepts/information-velocity.md": `---
type: concept
name: Information Velocity
---

See [[clarity-loop]] and [[signal-processing-cycle]].
`,
		"concepts/clarity-loop.md": `---
type: concept
name: Clarity Loop
---
`,
		"concepts/signal-processing-cycle.md": `---
type: concept
name: Signal Processing Cycle
---
`,
	})
	prepared, errs := Prepare(env.execSchema, `{ note(path: "concepts/information-velocity.md") { ... on concept { connectedConcepts { title } } } }`)
	require.Empty(t, errs)

	deps := env.deps(nil)
	deps.Store = &duplicateAmbientStore{Store: env.store}

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	note := result.Data["note"].(map[string]any)
	paths := titles(note["connectedConcepts"].([]any))
	require.Equal(t, []string{"Clarity Loop", "Signal Processing Cycle"}, paths)
}

type duplicateAmbientStore struct {
	*codeanchorsqlite.Store
}

func (s *duplicateAmbientStore) OntologyNeighborhood(ctx context.Context, request readmodel.NeighborhoodQuery) ([]codeanchorsqlite.OntologyEdgeRow, error) {
	rows, err := s.Store.OntologyNeighborhood(ctx, request)
	if err != nil || len(rows) == 0 {
		return rows, err
	}
	dup := rows[0]
	rows = append(rows, dup)
	return rows, nil
}

type relationBatchSpyStore struct {
	*codeanchorsqlite.Store
	structuralCalls      int
	ambientCalls         int
	maxStructuralSources int
	maxAmbientSources    int
}

func (s *relationBatchSpyStore) OntologyNeighborhood(ctx context.Context, request readmodel.NeighborhoodQuery) ([]codeanchorsqlite.OntologyEdgeRow, error) {
	if request.IncludeStructural {
		s.structuralCalls++
		if len(request.Sources) > s.maxStructuralSources {
			s.maxStructuralSources = len(request.Sources)
		}
	}
	if request.IncludeAmbient {
		s.ambientCalls++
		if len(request.Sources) > s.maxAmbientSources {
			s.maxAmbientSources = len(request.Sources)
		}
	}
	return s.Store.OntologyNeighborhood(ctx, request)
}
