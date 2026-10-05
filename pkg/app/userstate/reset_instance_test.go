package userstate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func widgetScope(host Scope, widget string) Scope { host.WidgetSlot = widget; return host }

func TestResetInstanceClearsUnmountedWidgetsAndKeepsOtherInstances(t *testing.T) {
	t.Parallel()
	ctx, root := context.Background(), t.TempDir()
	store := openTestStore(t, root)
	host := testScope("work")
	host.Slot = "detail/<é>"
	widgetA, widgetB := widgetScope(host, "table"), widgetScope(host, "chart")
	otherHost := host
	otherHost.Slot = "detail"
	otherView := host
	otherView.ViewID = "other"
	others := []Scope{widgetScope(otherHost, "table"), widgetScope(testScope("home"), "table"), widgetScope(otherView, "table")}
	for _, scope := range append([]Scope{widgetA, widgetB}, others...) {
		_, err := store.Patch(ctx, scope, 0, map[string]json.RawMessage{"density": json.RawMessage(`"compact"`)}, nil)
		require.NoError(t, err)
	}
	reset, err := store.ResetInstance(ctx, host, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, reset.Revision)
	for _, scope := range []Scope{widgetA, widgetB} {
		snapshot, err := store.Read(ctx, scope)
		require.NoError(t, err)
		require.Empty(t, snapshot.Values)
		require.True(t, snapshot.MigrationClosed)
		require.EqualValues(t, 2, snapshot.Revision)
	}
	for _, scope := range others {
		snapshot, err := store.Read(ctx, scope)
		require.NoError(t, err)
		require.Len(t, snapshot.Values, 1)
		require.EqualValues(t, 1, snapshot.Revision)
	}
	unknown := widgetScope(host, "never-mounted")
	before, err := store.Read(ctx, unknown)
	require.NoError(t, err)
	require.True(t, before.MigrationClosed)
	require.EqualValues(t, 1, before.Revision)
	_, err = store.Patch(ctx, unknown, 0, map[string]json.RawMessage{"density": json.RawMessage(`"old"`)}, nil)
	var conflict *ConflictError
	require.ErrorAs(t, err, &conflict)
	importedState, imported, err := store.ImportIfAbsent(ctx, unknown, "legacy:unknown-widget", map[string]json.RawMessage{"density": json.RawMessage(`"old"`)})
	require.NoError(t, err)
	require.False(t, imported)
	require.Empty(t, importedState.Values)
	_, imported, err = store.ImportIfAbsent(ctx, widgetScope(otherHost, "fresh"), "legacy:unknown-widget", nil)
	require.NoError(t, err)
	require.False(t, imported, "suppressed unknown-child import still claims the source")
	require.NoError(t, store.Close())
	store = openTestStore(t, root)
	after, err := store.Read(ctx, widgetScope(host, "another-new-widget"))
	require.NoError(t, err)
	require.True(t, after.MigrationClosed)
	require.EqualValues(t, 1, after.Revision)
	_, err = store.ResetInstance(ctx, widgetA, 2)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestExactWidgetResetAndHostPatchDoNotCloseOtherWidgetMigration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openTestStore(t, t.TempDir())
	host := testScope("work")
	_, err := store.Patch(ctx, host, 0, map[string]json.RawMessage{"value": json.RawMessage(`true`)}, nil)
	require.NoError(t, err)
	_, err = store.Reset(ctx, widgetScope(host, "a"), 0)
	require.NoError(t, err)
	state, imported, err := store.ImportIfAbsent(ctx, widgetScope(host, "b"), "legacy:b", map[string]json.RawMessage{"value": json.RawMessage(`true`)})
	require.NoError(t, err)
	require.True(t, imported)
	require.Len(t, state.Values, 1)
	reset, err := store.ResetInstance(ctx, host, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, reset.Revision)
	snapshot, err := store.Read(ctx, widgetScope(host, "b"))
	require.NoError(t, err)
	require.Empty(t, snapshot.Values)
}

func TestResetInstanceRacingWidgetWriteCannotRestorePriorState(t *testing.T) {
	t.Parallel()
	ctx, root := context.Background(), t.TempDir()
	a, b := openTestStore(t, root), openTestStore(t, root)
	host, widget := testScope("work"), widgetScope(testScope("work"), "table")
	_, err := a.Patch(ctx, widget, 0, map[string]json.RawMessage{"value": json.RawMessage(`true`)}, nil)
	require.NoError(t, err)
	start := make(chan struct{})
	patched, reset := make(chan error, 1), make(chan error, 1)
	go func() {
		<-start
		_, err := a.Patch(ctx, widget, 1, map[string]json.RawMessage{"value": json.RawMessage(`false`)}, nil)
		patched <- err
	}()
	go func() { <-start; _, err := b.ResetInstance(ctx, host, 0); reset <- err }()
	close(start)
	require.NoError(t, <-reset)
	patchErr := <-patched
	if patchErr != nil {
		var conflict *ConflictError
		require.ErrorAs(t, patchErr, &conflict)
	}
	final, err := a.Read(ctx, widget)
	require.NoError(t, err)
	require.Empty(t, final.Values)
	require.True(t, final.MigrationClosed)
	if patchErr == nil {
		require.EqualValues(t, 3, final.Revision)
	} else {
		require.EqualValues(t, 2, final.Revision)
	}
}
