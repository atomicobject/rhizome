package bootstrap

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/stretchr/testify/require"
)

func TestSessionOpeningWaitsForRequestedCodeInitialization(t *testing.T) {
	for _, cancelOpening := range []bool{false, true} {
		name := "code completes"
		if cancelOpening {
			name = "runtime closes"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
			require.NoError(t, err)
			require.NoError(t, store.Close())
			rt := newCodeOpeningRuntime(t, root)
			rt.disableSessionStore = false
			rt.sessionReady = make(chan struct{})
			rt.startWorker(rt.initSessionStoreOnly)
			waitCtx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			require.ErrorIs(t, rt.WaitForSession(waitCtx), context.DeadlineExceeded)
			require.Nil(t, rt.Snapshot().SessionStore)
			if cancelOpening {
				require.NoError(t, rt.Close())
				require.Nil(t, rt.Snapshot().SessionStore)
			} else {
				close(rt.codeReady)
				require.NoError(t, rt.WaitForSession(t.Context()))
				require.NotNil(t, rt.Snapshot().SessionStore)
			}
		})
	}
}

func TestSessionOnlyOpeningDoesNotWaitForCode(t *testing.T) {
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	require.NoError(t, store.Close())
	rt := newCodeOpeningRuntime(t, root)
	rt.requirements = RequireRuntimeCapabilities(RuntimeCapabilitySearch)
	rt.disableSessionStore = false
	rt.sessionReady = make(chan struct{})
	rt.startWorker(rt.initSessionStoreOnly)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, rt.WaitForSession(ctx))
	require.NotNil(t, rt.Snapshot().SessionStore)
}
