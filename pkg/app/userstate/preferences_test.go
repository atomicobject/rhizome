package userstate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T, root string) *Store {
	t.Helper()
	store, err := Open(context.Background(), root)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	return store
}

func testScope(group string) Scope {
	return Scope{ViewID: "tasks", Context: Context{Kind: viewconfig.MountKindGroup, Group: group}}
}

func TestTwoHandlesRejectStaleWritesAndPreserveIndependentKeys(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a, b := openTestStore(t, root), openTestStore(t, root)
	require.Same(t, a.writeMu, b.writeMu)
	scope := testScope("work")
	type outcome struct {
		snapshot Snapshot
		err      error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for i, store := range []*Store{a, b} {
		go func() {
			<-start
			key := []string{"density", "sort"}[i]
			snapshot, err := store.Patch(context.Background(), scope, 0, map[string]json.RawMessage{key: json.RawMessage(`null`)}, nil)
			results <- outcome{snapshot, err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil {
		first, second = second, first
	}
	require.NoError(t, first.err)
	var conflict *ConflictError
	require.ErrorAs(t, second.err, &conflict)
	require.Equal(t, first.snapshot, conflict.Current)
	require.EqualValues(t, 1, conflict.Current.Revision)
	missing := "density"
	if _, exists := conflict.Current.Values[missing]; exists {
		missing = "sort"
	}
	merged, err := b.Patch(context.Background(), scope, 1, map[string]json.RawMessage{missing: json.RawMessage(`false`)}, nil)
	require.NoError(t, err)
	require.Len(t, merged.Values, 2)
	require.EqualValues(t, 2, merged.Revision)
	deleted, err := a.Patch(context.Background(), scope, 2, nil, []string{missing})
	require.NoError(t, err)
	require.Len(t, deleted.Values, 1)
	for _, value := range deleted.Values {
		require.JSONEq(t, `null`, string(value))
	}
}

func TestConcurrentFreshOpensAndImportsClaimOneSourceAcrossScopes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var stores [2]*Store
	var openErrors [2]error
	var wg sync.WaitGroup
	for i := range stores {
		wg.Add(1)
		go func() { defer wg.Done(); stores[i], openErrors[i] = Open(context.Background(), root) }()
	}
	wg.Wait()
	for i := range stores {
		require.NoError(t, openErrors[i])
		defer stores[i].Close()
	}
	var imported [2]bool
	var importErrors [2]error
	for i := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, imported[i], importErrors[i] = stores[i].ImportIfAbsent(context.Background(), testScope([]string{"work", "home"}[i]), "legacy:tasks", map[string]json.RawMessage{"density": json.RawMessage(`"compact"`)})
		}()
	}
	wg.Wait()
	require.NoError(t, importErrors[0])
	require.NoError(t, importErrors[1])
	require.NotEqual(t, imported[0], imported[1])
	for i, group := range []string{"work", "home"} {
		snapshot, err := stores[i].Read(context.Background(), testScope(group))
		require.NoError(t, err)
		require.False(t, snapshot.MigrationClosed)
		if imported[i] {
			require.Len(t, snapshot.Values, 1)
		} else {
			require.Empty(t, snapshot.Values)
		}
	}
}

func TestResetAndChangesPreventLegacyResurrectionAfterRestart(t *testing.T) {
	t.Parallel()
	ctx, root := context.Background(), t.TempDir()
	store := openTestStore(t, root)
	scope := testScope("work")
	_, imported, err := store.ImportIfAbsent(ctx, scope, "legacy:a", map[string]json.RawMessage{"density": json.RawMessage(`"compact"`)})
	require.NoError(t, err)
	require.True(t, imported)
	reset, err := store.Reset(ctx, scope, 1)
	require.NoError(t, err)
	require.Empty(t, reset.Values)
	require.True(t, reset.MigrationClosed)
	require.NoError(t, store.Close())
	store = openTestStore(t, root)
	for _, id := range []string{"legacy:a", "legacy:b"} {
		snapshot, imported, err := store.ImportIfAbsent(ctx, scope, id, map[string]json.RawMessage{"density": json.RawMessage(`"old"`)})
		require.NoError(t, err)
		require.False(t, imported)
		require.Empty(t, snapshot.Values)
		require.EqualValues(t, 2, snapshot.Revision)
	}
	_, imported, err = store.ImportIfAbsent(ctx, testScope("home"), "legacy:b", map[string]json.RawMessage{"density": json.RawMessage(`"old"`)})
	require.NoError(t, err)
	require.False(t, imported, "even a suppressed import claims its old source")
	fresh := testScope("fresh")
	_, err = store.Reset(ctx, fresh, 0)
	require.NoError(t, err)
	_, imported, err = store.ImportIfAbsent(ctx, fresh, "legacy:c", nil)
	require.NoError(t, err)
	require.False(t, imported)
	changed := testScope("changed")
	_, err = store.Patch(ctx, changed, 0, map[string]json.RawMessage{"value": json.RawMessage(`42`)}, nil)
	require.NoError(t, err)
	snapshot, imported, err := store.ImportIfAbsent(ctx, changed, "legacy:d", map[string]json.RawMessage{"value": json.RawMessage(`13`)})
	require.NoError(t, err)
	require.False(t, imported)
	require.JSONEq(t, `42`, string(snapshot.Values["value"]))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "db.sqlite"), []byte("replaced disposable index"), 0o600))
	require.NoError(t, store.Close())
	store = openTestStore(t, root)
	read, err := store.Read(ctx, changed)
	require.NoError(t, err)
	require.Equal(t, snapshot, read)
}

func TestInvalidBatchesAndCanceledWritesLeavePriorSnapshotIntact(t *testing.T) {
	t.Parallel()
	store := openTestStore(t, t.TempDir())
	scope, ctx := testScope("work"), context.Background()
	initial, err := store.Patch(ctx, scope, 0, map[string]json.RawMessage{"value": json.RawMessage(`"original"`)}, nil)
	require.NoError(t, err)
	_, err = store.Patch(ctx, scope, 1, map[string]json.RawMessage{"value": json.RawMessage(`true`)}, []string{"value"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = store.Patch(ctx, scope, 1, map[string]json.RawMessage{"bad": json.RawMessage(`undefined`)}, nil)
	require.ErrorIs(t, err, ErrInvalid)
	large := json.RawMessage(`"` + strings.Repeat("x", MaxValueBytes-3) + `"`)
	_, err = store.Patch(ctx, scope, 1, map[string]json.RawMessage{"a": large, "b": large}, nil)
	require.ErrorIs(t, err, ErrInvalid, "resulting total state limit rolls back the transaction")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = store.Reset(canceled, scope, 1)
	require.ErrorIs(t, err, context.Canceled)
	read, err := store.Read(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, initial, read)
}

func TestScopeIdentityDistinguishesSubjectsAndIgnoresNodeOffsets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	store := openTestStore(t, root)
	base := Scope{ViewID: "arbitrary-unregistered", Context: Context{Kind: viewconfig.MountKindNode, Type: "Task", Ref: &ontology.NodeRef{NotePath: "Notes/./todo.md", Kind: ontology.NodeKindEmbedded, NodeID: "task1", Fragment: "old", Structural: "old", StartByte: 10}}}
	canonical, key, err := CanonicalScope(root, base)
	require.NoError(t, err)
	require.Equal(t, "Notes/todo.md", canonical.Context.Ref.NotePath)
	require.Empty(t, canonical.Context.Ref.Fragment)
	require.Empty(t, canonical.Context.Ref.Structural)
	require.Zero(t, canonical.Context.Ref.StartByte)
	_, err = store.Patch(context.Background(), base, 0, map[string]json.RawMessage{"value": json.RawMessage(`true`)}, nil)
	require.NoError(t, err)
	changed := base
	ref := *base.Context.Ref
	ref.Fragment, ref.Structural, ref.StartByte = "new", "new", 500
	changed.Context.Ref = &ref
	_, sameKey, err := CanonicalScope(root, changed)
	require.NoError(t, err)
	require.Equal(t, key, sameKey)
	snapshot, err := store.Read(context.Background(), changed)
	require.NoError(t, err)
	require.Len(t, snapshot.Values, 1)
	for _, change := range []func(*Scope){
		func(s *Scope) { s.ViewID = "other-view" },
		func(s *Scope) { s.Slot = "other-slot" },
		func(s *Scope) { r := *s.Context.Ref; r.NodeID = "task2"; s.Context.Ref = &r },
	} {
		other := base
		change(&other)
		snapshot, err := store.Read(context.Background(), other)
		require.NoError(t, err)
		require.Empty(t, snapshot.Values)
	}
	for _, scope := range []Scope{
		{ViewID: "$selection", Context: Context{Kind: viewconfig.MountKindStandalone}, Slot: "navigation"},
		{ViewID: "$selection", Context: Context{Kind: viewconfig.MountKindWorkspace}},
		{ViewID: "tasks", Context: Context{Kind: viewconfig.MountKindType, Type: "Task"}},
		{ViewID: "tasks", Context: Context{Kind: viewconfig.MountKindInterface, Interface: "Task"}},
		testScope("Task"),
	} {
		snapshot, err := store.Patch(context.Background(), scope, 0, map[string]json.RawMessage{"value": json.RawMessage(`false`)}, nil)
		require.NoError(t, err)
		require.EqualValues(t, 1, snapshot.Revision)
	}
	for _, notePath := range []string{"../outside.md", filepath.Join(root, "inside.md"), filepath.Join(filepath.Dir(root), "outside.md"), `C:\outside.md`} {
		bad := base
		r := *base.Context.Ref
		r.NotePath = notePath
		bad.Context.Ref = &r
		_, _, err := CanonicalScope(root, bad)
		require.True(t, errors.Is(err, ErrInvalid))
	}
	base.Context.Type = "*"
	_, _, err = CanonicalScope(root, base)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestMultipleLegacySourcesFillMissingKeysBeforePersonalChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openTestStore(t, t.TempDir())
	scope := testScope("work")
	first, imported, err := store.ImportIfAbsent(ctx, scope, "legacy:query", map[string]json.RawMessage{"search": json.RawMessage(`"foo"`), "density": json.RawMessage(`"normal"`)})
	require.NoError(t, err)
	require.True(t, imported)
	require.False(t, first.MigrationClosed)
	second, imported, err := store.ImportIfAbsent(ctx, scope, "legacy:layout", map[string]json.RawMessage{"density": json.RawMessage(`"compact"`), "variant": json.RawMessage(`"card"`)})
	require.NoError(t, err)
	require.True(t, imported)
	require.False(t, second.MigrationClosed)
	require.Len(t, second.Values, 3)
	require.JSONEq(t, `"normal"`, string(second.Values["density"]))
	require.JSONEq(t, `"card"`, string(second.Values["variant"]))
	_, imported, err = store.ImportIfAbsent(ctx, testScope("home"), "legacy:layout", map[string]json.RawMessage{"variant": json.RawMessage(`"card"`)})
	require.NoError(t, err)
	require.False(t, imported)
	patched, err := store.Patch(ctx, scope, second.Revision, nil, []string{"variant"})
	require.NoError(t, err)
	require.True(t, patched.MigrationClosed)
	third, imported, err := store.ImportIfAbsent(ctx, scope, "legacy:custom", map[string]json.RawMessage{"variant": json.RawMessage(`"table"`)})
	require.NoError(t, err)
	require.False(t, imported)
	require.NotContains(t, third.Values, "variant")
}
