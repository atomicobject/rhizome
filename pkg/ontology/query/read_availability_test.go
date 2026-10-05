package query

import (
	"context"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNodeByRefRemainsAvailableDuringUnrelatedOwnershipReconciliation(t *testing.T) {
	ctx := context.Background()
	env := newCustomQueryTestEnv(t, `type Item @node(paths: ["notes/*.md"]) { name: String }`, map[string]string{
		"notes/example.md": "---\nname: Example\n---\n# Example\nStill exists on disk.\n",
	})
	prepared, errs := Prepare(env.execSchema, `{ node(ref: "notes/example.md") { path title } }`)
	require.Empty(t, errs)

	before := Execute(ctx, env.deps(nil), env.schema, prepared)
	require.Empty(t, before.Errors)
	require.NotNil(t, before.Data["node"])

	_, err := env.store.ApplyOwnershipTransitions(ctx, []semdb.OwnershipTransition{{
		Path:   "notes/other.md",
		Target: semdb.OwnershipTargetUnowned,
	}})
	require.NoError(t, err)

	state, err := env.store.GetNoteMetadataState(ctx)
	require.NoError(t, err)
	require.False(t, state.Ready)
	durable, err := env.store.DurableNoteMetadataRowsByPaths(ctx, []string{"notes/example.md"})
	require.NoError(t, err)
	require.Contains(t, durable, "notes/example.md", "the requested eligible row remains durably published")

	during := Execute(ctx, env.deps(nil), env.schema, prepared)
	require.Empty(t, during.Errors)
	require.NotNil(t, during.Data["node"], "unrelated reconciliation must not turn an existing note into a miss")

	_, err = env.store.ApplyOwnershipTransitions(ctx, []semdb.OwnershipTransition{{
		Path:   "notes/example.md",
		Target: semdb.OwnershipTargetUnowned,
	}})
	require.NoError(t, err)
	retired := Execute(ctx, env.deps(nil), env.schema, prepared)
	require.Empty(t, retired.Errors)
	require.Nil(t, retired.Data["node"], "a durably retired requested path must not reuse its prior projection")

	_, err = testNoteMetadataIndexer(t).EnsureIndexed(ctx, obsidian.VaultDefinition{Path: env.root}, &obsidian.Note{}, env.store)
	require.NoError(t, err)
	after := Execute(ctx, env.deps(nil), env.schema, prepared)
	require.Empty(t, after.Errors)
	require.NotNil(t, after.Data["node"])
}

func TestPreparedQueryExactNotePathsRejectsBroadAndMixedRoots(t *testing.T) {
	env := newCustomQueryTestEnv(t, `type Item @node(paths: ["notes/*.md"]) { name: String related: Item @link }`, map[string]string{
		"notes/example.md": "# Example\n",
	})

	exact, errs := PrepareWithVariables(env.execSchema, `query($ref: String!) { node(ref: $ref) { path } }`, map[string]any{"ref": "notes/example.md"})
	require.Empty(t, errs)
	paths, ok := exact.ExactNotePaths()
	require.True(t, ok)
	require.Equal(t, []string{"notes/example.md"}, paths)

	for _, raw := range []string{
		`{ node(ref: "notes/example.md#details") { path } }`,
		`{ node(ref: "notes/example.md#^example-block") { path } }`,
		`{ notes(type: "Item", first: 1) { nodes { path } } }`,
		`{ node(ref: "notes/example.md") { path } notes(type: "Item", first: 1) { nodes { path } } }`,
		`{ node(ref: "notes/example.md") { __typename path } }`,
		`{ note(path: "notes/example.md") { resolvedType } }`,
		`{ note(path: "notes/example.md") { ref { typeName } } }`,
		`{ node(ref: "notes/example.md") { ... on Item { path } } }`,
		`{ node(ref: "notes/example.md") { ...ItemPath } } fragment ItemPath on Item { path }`,
		`{ node(ref: "notes/example.md") { workspace { version } } }`,
		`{ node(ref: "notes/example.md") { neighborhood { nodes { ref { notePath } } } } }`,
		`{ node(ref: "notes/example.md") { localGraph { truncated } } }`,
		`{ node(ref: "notes/example.md") { ... on Item { related { path } } } }`,
	} {
		prepared, prepareErrs := Prepare(env.execSchema, raw)
		require.Empty(t, prepareErrs)
		_, ok := prepared.ExactNotePaths()
		require.False(t, ok)
	}
}

func TestPreparedQueryExactNotePathsAllowsAuthoredTypeNameField(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
interface Note {
  typeName: String
}

type Item @node(paths: ["notes/*.md"]) {
  name: String
}
`, map[string]string{
		"notes/example.md": "---\ntypeName: authored value\n---\n# Example\n",
	})

	prepared, errs := Prepare(env.execSchema, `{ note(path: "notes/example.md") { typeName } }`)
	require.Empty(t, errs)
	paths, ok := prepared.ExactNotePaths()
	require.True(t, ok)
	require.Equal(t, []string{"notes/example.md"}, paths)
}
