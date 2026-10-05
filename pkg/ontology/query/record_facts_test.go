package query

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const recordFactsSchema = `
interface Grouping { name: String }
interface SubGrouping implements Grouping { name: String }
interface Mixed { name: String }

type Area implements Grouping & Mixed @node(paths: ["areas/*.md"]) {
  name: String
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}

type StoriesSection implements Section {
  stories: [Story!] @contains(level: H3)
}

type Story implements Section @node(locator: EMBEDDED) {
  summary: String @field
}

type Log implements Mixed @node(paths: ["logs/*.md"]) {
  name: String
  updatedAt: Date @field(source: "updated")
}
`

func newRecordFactsEnv(t *testing.T) *queryTestEnv {
	t.Helper()
	return newCustomQueryTestEnv(t, recordFactsSchema, map[string]string{
		"areas/a.md": "---\ntype: Area\nname: A\n---\n# A\n\n## Stories\n\n### First story\nsummary:: First\n",
		"areas/b.md": "---\ntype: Area\nname: B\n---\n# B\n",
		"logs/l.md":  "---\ntype: Log\nname: L\nupdated: 2026-01-02\n---\n# L\n",
	})
}

func fieldTypeOn(t *testing.T, env *queryTestEnv, typeName, field string) string {
	t.Helper()
	def := env.execSchema.Schema.Types[typeName]
	require.NotNil(t, def, typeName)
	if found := def.Fields.ForName(field); found != nil {
		return found.Type.String()
	}
	return ""
}

func TestRecordFactsSchemaRespectsAuthoredFields(t *testing.T) {
	env := newRecordFactsEnv(t)
	for _, typeName := range []string{"Area", "Story", "Grouping", "SubGrouping"} {
		require.Equal(t, "DateTime", fieldTypeOn(t, env, typeName, "updatedAt"), typeName)
		require.Equal(t, "Int!", fieldTypeOn(t, env, typeName, "issueCount"), typeName)
	}
	for _, typeName := range []string{"NoteNode", "Note"} {
		require.Empty(t, fieldTypeOn(t, env, typeName, "updatedAt"), "Log authors updatedAt, so %s cannot declare the runtime one", typeName)
		require.Equal(t, "Int!", fieldTypeOn(t, env, typeName, "issueCount"), typeName)
	}
	require.Equal(t, "Date", fieldTypeOn(t, env, "Log", "updatedAt"), "an authored field wins")
	require.Equal(t, "Int!", fieldTypeOn(t, env, "Log", "issueCount"))
	require.Empty(t, fieldTypeOn(t, env, "Mixed", "updatedAt"), "implementors disagree, so the interface omits it")
	require.Equal(t, "Int!", fieldTypeOn(t, env, "Mixed", "issueCount"))
	require.Empty(t, fieldTypeOn(t, env, "StoriesSection", "updatedAt"), "plain sections are not records")
}

// Every interface shape must compile to a valid query schema: a parent
// interface may declare a record fact only when each sub-interface does too.
func TestRecordFactsSchemaCompilesForInterfaceHierarchies(t *testing.T) {
	cases := []struct {
		name   string
		schema string
		want   map[string]string // type.field -> GraphQL type, "" for absent
	}{
		{
			name: "sub-interface without implementors",
			schema: `
interface I { name: String }
interface J implements I { name: String }
type A implements I @node(paths: ["a/*.md"]) { name: String }
`,
			want: map[string]string{"I.updatedAt": "DateTime", "J.updatedAt": "DateTime", "J.issueCount": "Int!"},
		},
		{
			name: "implementors only through the sub-interface",
			schema: `
interface I { name: String }
interface J implements I { name: String }
type A implements J & I @node(paths: ["a/*.md"]) { name: String }
`,
			want: map[string]string{"I.updatedAt": "DateTime", "J.updatedAt": "DateTime", "A.updatedAt": "DateTime"},
		},
		{
			name: "sub-interface without implementors authors the name",
			schema: `
interface I { name: String }
interface J implements I { name: String updatedAt: Date }
type A implements I @node(paths: ["a/*.md"]) { name: String }
`,
			want: map[string]string{"I.updatedAt": "", "I.issueCount": "Int!", "J.updatedAt": "Date", "A.updatedAt": "DateTime"},
		},
		{
			name: "authored Note interface without note types",
			schema: `
interface Note { updatedAt: Date }
`,
			want: map[string]string{"Note.updatedAt": "Date", "NoteNode.updatedAt": "Date", "Note.issueCount": "Int!"},
		},
		{
			name: "interface without implementors",
			schema: `
interface I { name: String }
type A @node(paths: ["a/*.md"]) { name: String }
`,
			want: map[string]string{"I.updatedAt": "DateTime", "I.issueCount": "Int!"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeQueryTestConfig(t, root)
			writeQueryTestSchema(t, root, tc.schema)
			schema, err := ontology.LoadSchema(root)
			require.NoError(t, err)
			execSchema, err := BuildExecutableSchema(schema)
			require.NoError(t, err)
			env := &queryTestEnv{execSchema: execSchema}
			for key, want := range tc.want {
				typeName, field, _ := strings.Cut(key, ".")
				require.Equal(t, want, fieldTypeOn(t, env, typeName, field), key)
			}
		})
	}
}

func TestRecordFactsResolveFromIndexAndValidationSnapshot(t *testing.T) {
	env := newRecordFactsEnv(t)
	run := func(query string) map[string]any {
		t.Helper()
		prepared, errs := Prepare(env.execSchema, query)
		require.Empty(t, errs)
		result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
		require.Empty(t, result.Errors)
		return result.Data
	}

	before := run(`{ area(first: 10) { path issueCount } story(first: 10) { ref { nodeId } issueCount } }`)
	for _, row := range before["area"].([]any) {
		require.Equal(t, 0, row.(map[string]any)["issueCount"], "no published validation yet")
	}
	stories := before["story"].([]any)
	require.Len(t, stories, 1)
	storyNodeID := stories[0].(map[string]any)["ref"].(map[string]any)["nodeId"].(string)
	require.NotEmpty(t, storyNodeID)

	publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
		SelectedChecks: []string{"ontology"}, IssueCount: 3, AffectedNoteCount: 1,
		Checks: []codeanchorsqlite.ValidationCheckSnapshot{{Check: "ontology", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 3}},
		Diagnostics: []codeanchorsqlite.ValidationDiagnostic{
			{IssueKey: "a:1", Check: "ontology", Code: "x", PrimaryPath: "areas/a.md", AffectedNotePaths: []string{"areas/a.md"}},
			{IssueKey: "a:2", Check: "ontology", Code: "y", PrimaryPath: "areas/a.md", AffectedNotePaths: []string{"areas/a.md"}},
			{IssueKey: "s:1", Check: "ontology", Code: "z", PrimaryPath: "areas/a.md", AffectedNotePaths: []string{"areas/a.md"}, AffectedNodeIDs: []string{storyNodeID}},
		},
	})

	data := run(`{
  area(first: 10) { path updatedAt issueCount }
  story(first: 10) { updatedAt issueCount }
  log(first: 10) { updatedAt issueCount }
}`)
	info, err := os.Stat(filepath.Join(env.root, "areas", "a.md"))
	require.NoError(t, err)
	wantUpdated := info.ModTime().UTC().Truncate(time.Second).Format(time.RFC3339)

	byPath := map[string]map[string]any{}
	for _, row := range data["area"].([]any) {
		byPath[row.(map[string]any)["path"].(string)] = row.(map[string]any)
	}
	require.Equal(t, wantUpdated, byPath["areas/a.md"]["updatedAt"])
	require.Equal(t, 3, byPath["areas/a.md"]["issueCount"], "two note issues plus the embedded story's issue, which belongs to the note too")
	require.Equal(t, 0, byPath["areas/b.md"]["issueCount"])

	story := data["story"].([]any)[0].(map[string]any)
	require.Equal(t, wantUpdated, story["updatedAt"], "embedded records report their source note's modification")
	require.Equal(t, 1, story["issueCount"])

	log := data["log"].([]any)[0].(map[string]any)
	require.Equal(t, "2026-01-02", log["updatedAt"], "the authored field still resolves")
}

type scopeSummaryCountingStore struct {
	*codeanchorsqlite.Store
	calls int
}

func (s *scopeSummaryCountingStore) GetValidationScopeSummaries(ctx context.Context, request codeanchorsqlite.ValidationScopeSummaryRequest) (codeanchorsqlite.ValidationScopeSummaryResponse, error) {
	s.calls++
	return s.Store.GetValidationScopeSummaries(ctx, request)
}

// Nested record facts batch across parents: a link target, a reverse list,
// and section-field records under every parent cost one validation summary
// read for the whole response and no metadata reads beyond the same selection
// without the facts.
func TestRecordFactsNestedSelectionsBatchAcrossParents(t *testing.T) {
	const schema = `
type Area @node(paths: ["areas/*.md"]) {
  name: String
  parent: Area @link
  children: [Area!] @reverse(field: "parent")
  stories: StoriesSection @contains(level: H2, heading: "Stories")
}
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Story implements Section @node(locator: EMBEDDED) { summary: String @field }
`
	const query = `{
  area(first: 100) {
    path
    parent { updatedAt issueCount }
    children { updatedAt issueCount }
    stories { stories { title updatedAt issueCount } }
  }
}`
	const bareQuery = `{
  area(first: 100) {
    path
    parent { path }
    children { path }
    stories { stories { title } }
  }
}`
	reads := func(t *testing.T, query string) (summaries, metadata int, data map[string]any) {
		t.Helper()
		notes := map[string]string{"areas/root.md": "---\ntype: Area\nname: Root\n---\n# Root\n"}
		for i := range 12 {
			notes[fmt.Sprintf("areas/a%02d.md", i)] = fmt.Sprintf("---\ntype: Area\nname: A%02d\nparent: \"[[root]]\"\n---\n# A%02d\n\n## Stories\n\n### First %02d\nsummary:: one\n\n### Second %02d\nsummary:: two\n", i, i, i, i)
		}
		env := newCustomQueryTestEnv(t, schema, notes)
		publishQueryValidationSnapshot(t, env.store, codeanchorsqlite.ValidationSnapshot{
			SelectedChecks: []string{"ontology"}, IssueCount: 1, AffectedNoteCount: 1,
			Checks:      []codeanchorsqlite.ValidationCheckSnapshot{{Check: "ontology", Outcome: codeanchorsqlite.ValidationCheckOutcomeCompleted, IssueCount: 1}},
			Diagnostics: []codeanchorsqlite.ValidationDiagnostic{{IssueKey: "r:1", Check: "ontology", Code: "x", PrimaryPath: "areas/root.md", AffectedNotePaths: []string{"areas/root.md"}}},
		})
		prepared, errs := Prepare(env.execSchema, query)
		require.Empty(t, errs)
		deps := env.deps(nil)
		store := &scopeSummaryCountingStore{Store: env.store}
		deps.Store = store
		exact := deps.ExactNoteMetadataRows
		deps.ExactNoteMetadataRows = func(ctx context.Context, paths []string) (map[string]codeanchorsqlite.NoteMetadataRow, error) {
			metadata++
			return exact(ctx, paths)
		}
		result := Execute(context.Background(), deps, env.schema, prepared)
		require.Empty(t, result.Errors)
		return store.calls, metadata, result.Data
	}

	summaries, metadata, data := reads(t, query)
	_, bareMetadata, _ := reads(t, bareQuery)
	require.Equal(t, 1, summaries, "one validation summary read for every nested record")
	require.Equal(t, bareMetadata, metadata, "record facts add no metadata reads to the same selection")

	rows := data["area"].([]any)
	require.Len(t, rows, 13)
	for _, item := range rows {
		row := item.(map[string]any)
		if row["path"] == "areas/root.md" {
			require.Nil(t, row["parent"])
			require.Len(t, row["children"].([]any), 12)
			continue
		}
		parent := row["parent"].(map[string]any)
		require.Equal(t, 1, parent["issueCount"])
		require.NotEmpty(t, parent["updatedAt"])
		stories := row["stories"].(map[string]any)["stories"].([]any)
		require.Len(t, stories, 2)
		for _, story := range stories {
			require.Equal(t, 0, story.(map[string]any)["issueCount"])
			require.NotEmpty(t, story.(map[string]any)["updatedAt"])
		}
	}
}

// Typed roots sort by the updatedAt record fact, so a capped read takes the
// most recently modified records; embedded records sort by their note's time.
func TestRootsSortByUpdatedAt(t *testing.T) {
	env := newCustomQueryTestEnv(t, recordFactsSchema, map[string]string{
		"areas/a.md": "---\ntype: Area\nname: A\n---\n# A\n\n## Stories\n\n### A story\nsummary:: a\n",
		"areas/b.md": "---\ntype: Area\nname: B\n---\n# B\n\n## Stories\n\n### B story\nsummary:: b\n",
		"areas/c.md": "---\ntype: Area\nname: C\n---\n# C\n\n## Stories\n\n### C story\nsummary:: c\n",
	})
	// Modification times out of path order, indexed like any edit.
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for path, offset := range map[string]time.Duration{"areas/a.md": time.Hour, "areas/b.md": 3 * time.Hour, "areas/c.md": 2 * time.Hour} {
		require.NoError(t, os.Chtimes(filepath.Join(env.root, path), base.Add(offset), base.Add(offset)))
	}
	require.NoError(t, env.noteMetadata.SyncPaths(context.Background(), obsidian.VaultDefinition{Path: env.root}, &obsidian.Note{}, env.store, []string{"areas/a.md", "areas/b.md", "areas/c.md"}, nil))
	deps := env.deps(nil)
	prepared, errs := Prepare(env.execSchema, `{
  area(first: 2, sort: [{field: "updatedAt", direction: desc}]) { path }
  story(first: 10, sort: [{field: "updatedAt", direction: desc}]) { title }
}`)
	require.Empty(t, errs)
	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	var areas, stories []string
	for _, row := range result.Data["area"].([]any) {
		areas = append(areas, row.(map[string]any)["path"].(string))
	}
	for _, row := range result.Data["story"].([]any) {
		stories = append(stories, row.(map[string]any)["title"].(string))
	}
	require.Equal(t, []string{"areas/b.md", "areas/c.md"}, areas, "newest first, before the first: cap")
	require.Equal(t, []string{"B story", "C story", "A story"}, stories)

	// The store orders by modification time before its limit, so a type with
	// more records than the residual candidate cap still yields its newest.
	prepared, errs = Prepare(env.execSchema, `{ ontology {
  area: queryPlan(type: "Area", first: 2, sort: [{field: "updatedAt", direction: desc}]) { pushedSort residualSort }
} }`)
	require.Empty(t, errs)
	result = Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	plans := result.Data["ontology"].(map[string]any)
	require.Equal(t, map[string]any{"pushedSort": []any{"updatedAt desc"}, "residualSort": []any{}}, plans["area"])
}
