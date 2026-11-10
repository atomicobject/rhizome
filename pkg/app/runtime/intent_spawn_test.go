package runtime

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpawnedChildRegistrationPreservesElectionAndCancellation(t *testing.T) {
	registry, vault := intentRegistry(t)
	ctx := t.Context()
	requireSpawnIntent(t, registry, vault, "child")
	require.NoError(t, registry.recordSpawnedChild(ctx, vault, "child", os.Getpid()))
	manifest := InstanceManifest{InstanceID: InstanceID(vault), VaultPath: vault, PID: os.Getpid(), RunID: "child"}
	require.NoError(t, registry.ElectIntent(ctx, vault, "child", true, manifest))
	require.NoError(t, registry.recordSpawnedChild(ctx, vault, "child", os.Getpid()))
	_, cancelled, err := registry.CancelIntent(ctx, vault, "child")
	require.NoError(t, err)
	require.True(t, cancelled)
	require.ErrorIs(t, registry.recordSpawnedChild(ctx, vault, "child", os.Getpid()), ErrStartCancelled)
	intent, found, err := registry.StartIntentFor(ctx, vault)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, intent.Cancelled)
	require.Equal(t, os.Getpid(), intent.OwnerPID)
}

func TestSpawnedChildRegistrationCannotReplaceAnotherOwner(t *testing.T) {
	registry, vault := intentRegistry(t)
	ctx := t.Context()
	requireSpawnIntent(t, registry, vault, "old")
	requireSpawnIntent(t, registry, vault, "new")
	require.ErrorIs(t, registry.recordSpawnedChild(ctx, vault, "old", os.Getpid()), ErrStartCancelled)
	require.NoError(t, registry.recordSpawnedChild(ctx, vault, "new", os.Getpid()))
	require.ErrorIs(t, registry.recordSpawnedChild(ctx, vault, "new", os.Getpid()+1), ErrStartCancelled)
	intent, found, err := registry.StartIntentFor(ctx, vault)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "new", intent.Token)
	require.Equal(t, os.Getpid(), intent.OwnerPID)
}

func TestCancelledSpawnCannotRegisterChild(t *testing.T) {
	registry, vault := intentRegistry(t)
	requireSpawnIntent(t, registry, vault, "child")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, registry.recordSpawnedChild(ctx, vault, "child", os.Getpid()), context.Canceled)
	intent, found, err := registry.StartIntentFor(t.Context(), vault)
	require.NoError(t, err)
	require.True(t, found)
	require.Zero(t, intent.OwnerPID)
}
