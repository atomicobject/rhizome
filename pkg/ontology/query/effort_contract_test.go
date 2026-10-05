package query

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEffortContract_MixedFormatsAndExplicitComponents(t *testing.T) {
	schema, err := os.ReadFile(filepath.Join("..", "..", "..", "pkg", "app", "cli", "init", "templates", "starters", "agentic-engineering", "rhizome", "ontology", "spec-driven.graphql"))
	require.NoError(t, err)
	const folder = "docs/efforts/2026-09-13-10-00-example/"
	const entry = folder + "2026-09-13-10-00-example-effort.html"
	storyURI := "story.md"
	if obsidian.IsCaseInsensitiveFS() {
		storyURI = "STORY.MD"
	}
	env := newCustomQueryTestEnv(t, string(schema)+`
type StoryFixture @node(paths: ["docs/story.md"]) {
 stories: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`, map[string]string{
		".rhizome/config.yml": "notes:\n  includes: [\"**/*.md\", \"**/*.html\"]\n",
		"docs/efforts/2026-09-12-10-00-old.md": `---
id: EFF-2026-09-12-10-00
name: Old effort
summary: Historical Markdown
status: complete
created-at: 2026-09-12T10:00:00Z
---
## Stories In Scope (Frozen)
[[story#^story-one]] [[story#^criterion-one]]
`,
		entry:                            `<html><head><title>Folder effort</title><script id="rhizome-metadata" type="application/json">{"id":"EFF-2026-09-13-10-00","name":"Folder effort","summary":"HTML workspace","status":"planned","created-at":"2026-09-13T10:00:00Z","implementation-plan":"` + folder + `plan.html","work-log":"` + folder + `work-log.md","materials":["` + folder + `materials/report.md"],"governing-specs":["docs/specs/Lower.md","docs/specs/Upper.MD","docs/specs/Title.Md","docs/specs/Mixed.mD"]}</script></head><body><a href="../../` + storyURI + `#^story-one">Selected story</a><a href="../../` + storyURI + `#^criterion-one">Criterion</a></body></html>`,
		folder + "plan.html":             `<html><head><title>Plan</title></head><body>Plan content</body></html>`,
		folder + "work-log.md":           "# Work log\n",
		folder + "materials/report.md":   "# Report\n",
		folder + "materials/unlinked.md": "# Unlinked collateral\n",
		"docs/story.md":                  "---\ntype: StoryFixture\n---\n## User Stories\n### US1 - Story\n- id:: ^story-one\n- summary:: Outcome\n- status:: ready\n#### Acceptance Criteria\n- Observable result ^criterion-one\n",
	})
	// These paths pass through collection discovery, provider projection, the
	// shipped type selectors, and the indexed governing-spec relationship.
	specPaths := []string{"docs/specs/Lower.md", "docs/specs/Upper.MD", "docs/specs/Title.Md", "docs/specs/Mixed.mD"}
	for index, path := range specPaths {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(env.root, path)), 0o755))
		body := fmt.Sprintf("---\ntype: TechnicalSpec\nid: SPEC-%d\naliases: [SPEC-%d]\nsummary: Case-preserving contract\nspec-status: active\n---\n# Contract\n", 9100+index, 9100+index)
		require.NoError(t, os.WriteFile(filepath.Join(env.root, path), []byte(body), 0o644))
	}
	vault := obsidian.VaultDefinition{Root: env.root, Includes: []string{"**/*.md", "**/*.html"}}
	_, err = env.noteMetadata.EnsureIndexed(context.Background(), vault, &obsidian.Note{}, env.store)
	require.NoError(t, err)
	indexed, err := ontology.BuildIndexWithStore(context.Background(), env.noteMetadata, vault, &obsidian.Note{}, env.store, env.schema, "mixed-formats")
	require.NoError(t, err)
	require.NoError(t, env.store.ReplaceOntologySnapshot(context.Background(), codeanchorsqlite.OntologySnapshot{
		Assessments: indexed.AssessmentRows, NoteTypes: indexed.NoteTypes, Edges: indexed.Edges,
		SchemaState: codeanchorsqlite.OntologySchemaState{SchemaHash: env.schema.Hash, NotesHash: indexed.NotesHash, LoadedAt: 1, Ready: true},
	}))
	require.NoError(t, env.store.ReplaceOntologyNodeReadModel(context.Background(), codeanchor.IntelOntologyNodeReadModel{
		NotePaths: indexed.NodePaths, Nodes: indexed.Nodes, FieldValues: indexed.NodeFieldValues,
	}))
	deps := env.deps(nil)
	deps.VaultDef = vault
	deps.NoteFormats, err = builtin.NewRuntime()
	require.NoError(t, err)
	deps.Service = ontology.NewService(vault, &obsidian.Note{}, env.store, env.schema)
	t.Run("governing specs preserve Markdown extension casing", func(t *testing.T) {
		prepared, errs := Prepare(env.execSchema, `{ effortWorkspace { governingSpecs { ... on NoteNode { path resolvedType } } } notes(type: "SpecLike") { nodes { path resolvedType } } }`)
		require.Empty(t, errs)
		result := Execute(context.Background(), deps, env.schema, prepared)
		require.Empty(t, result.Errors)
		want := make([]any, 0, len(specPaths))
		for _, path := range specPaths {
			want = append(want, map[string]any{"path": path, "resolvedType": "TechnicalSpec"})
		}
		workspace := result.Data["effortWorkspace"].([]any)[0].(map[string]any)
		require.ElementsMatch(t, want, workspace["governingSpecs"])
		require.ElementsMatch(t, want, result.Data["notes"].(map[string]any)["nodes"])
	})
	t.Run("provider section links respect snapshot ownership", func(t *testing.T) {
		loaders, err := newLoaders(deps, env.schema)
		require.NoError(t, err)
		exec := &executor{deps: deps, loaders: loaders}
		cache, err := exec.notePathCacheForSections(context.Background())
		require.NoError(t, err)
		scanned, _, err := loaders.snapshot(context.Background())
		require.NoError(t, err)
		links := make(map[string][]string)
		add := func(key, source string) { links[key] = append(links[key], source) }
		require.NoError(t, exec.addProviderInboundSectionNeighbors(context.Background(), cache, scanned, add))
		require.Equal(t, map[string][]string{
			inboundSectionNeighborKey("docs/story.md", "block:story-one"):     {entry},
			inboundSectionNeighborKey("docs/story.md", "block:criterion-one"): {entry},
		}, links)

		// Catalog ownership, rather than format identity, prevents a second scan.
		scanned[entry] = &noteRecord{Path: entry}
		clear(links)
		require.NoError(t, exec.addProviderInboundSectionNeighbors(context.Background(), cache, scanned, add))
		require.Empty(t, links)
	})
	t.Run("missing provider source preserves healthy section links", func(t *testing.T) {
		missingPath := filepath.Join(env.root, folder+"plan.html")
		movedPath := missingPath + ".unavailable"
		loaders, err := newLoaders(deps, env.schema)
		require.NoError(t, err)
		exec := &executor{deps: deps, loaders: loaders}
		cache, err := exec.notePathCacheForSections(context.Background())
		require.NoError(t, err)
		scanned, _, err := loaders.snapshot(context.Background())
		require.NoError(t, err)
		require.NoError(t, os.Rename(missingPath, movedPath))
		t.Cleanup(func() { require.NoError(t, os.Rename(movedPath, missingPath)) })
		links := make(map[string][]string)
		require.NoError(t, exec.addProviderInboundSectionNeighbors(context.Background(), cache, scanned, func(key, source string) {
			links[key] = append(links[key], source)
		}))
		require.Equal(t, map[string][]string{
			inboundSectionNeighborKey("docs/story.md", "block:story-one"):     {entry},
			inboundSectionNeighborKey("docs/story.md", "block:criterion-one"): {entry},
		}, links)
	})
	for _, missingFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("unexpected provider error missingFirst=%t", missingFirst), func(t *testing.T) {
			failure := errors.New("provider projection failed")
			reader := &sectionNeighborFailureReader{Note: &obsidian.Note{}}
			testDeps := deps
			testDeps.NoteReader = reader
			loaders, err := newLoaders(testDeps, env.schema)
			require.NoError(t, err)
			exec := &executor{deps: testDeps, loaders: loaders}
			cache, err := exec.notePathCacheForSections(context.Background())
			require.NoError(t, err)
			scanned, _, err := loaders.snapshot(context.Background())
			require.NoError(t, err)
			reader.failures = map[string]error{folder + "plan.html": failure}
			if missingFirst {
				reader.failures[entry] = errors.New(obsidian.NoteDoesNotExistError)
			}
			err = exec.addProviderInboundSectionNeighbors(context.Background(), cache, scanned, func(string, string) {})
			require.ErrorIs(t, err, failure)
		})
	}
	prepared, errs := Prepare(env.execSchema, `{ efforts: notes(type: "Effort") { nodes { resolvedType ... on Effort { id name status } } } effortNote { id } storyFixture { stories { stories { efforts { id } acceptanceCriteria { criteria { efforts { id } } } } } } effortWorkspace { id } }`)
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Len(t, result.Data["efforts"].(map[string]any)["nodes"].([]any), 2)
	require.Len(t, result.Data["effortNote"].([]any), 1)
	workspaces := result.Data["effortWorkspace"].([]any)
	require.Len(t, workspaces, 1)
	rows := result.Data["efforts"].(map[string]any)["nodes"].([]any)
	require.ElementsMatch(t, []any{
		map[string]any{"resolvedType": "EffortNote", "id": "EFF-2026-09-12-10-00", "name": "Old effort", "status": "complete"},
		map[string]any{"resolvedType": "EffortWorkspace", "id": "EFF-2026-09-13-10-00", "name": "Folder effort", "status": "planned"},
	}, rows)
	story := result.Data["storyFixture"].([]any)[0].(map[string]any)["stories"].(map[string]any)["stories"].([]any)[0].(map[string]any)
	require.Len(t, story["efforts"].([]any), 2)
	criteria := story["acceptanceCriteria"].(map[string]any)["criteria"].([]any)
	require.Len(t, criteria, 1)
	require.Len(t, criteria[0].(map[string]any)["efforts"].([]any), 2)
	prepared, errs = Prepare(env.execSchema, `{ effortWorkspace { implementationPlan { path } workLog { path } materials { path } } }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	workspace := result.Data["effortWorkspace"].([]any)[0].(map[string]any)
	require.Equal(t, folder+"plan.html", workspace["implementationPlan"].(map[string]any)["path"])
	require.Equal(t, folder+"work-log.md", workspace["workLog"].(map[string]any)["path"])
	require.Equal(t, []any{map[string]any{"path": folder + "materials/report.md"}}, workspace["materials"])
}

type sectionNeighborFailureReader struct {
	*obsidian.Note
	failures map[string]error
}

func (r *sectionNeighborFailureReader) GetContents(vault obsidian.VaultDefinition, path string) (string, error) {
	if err := r.failures[path]; err != nil {
		return "", err
	}
	return r.Note.GetContents(vault, path)
}
