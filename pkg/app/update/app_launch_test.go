package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func TestPortableAppLaunchDoesNotWaitForGUIExit(t *testing.T) {
	self, err := os.Executable()
	require.NoError(t, err)
	directory := t.TempDir()
	ready := filepath.Join(directory, "ready")
	release := filepath.Join(directory, "release")
	done := filepath.Join(directory, "done")
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	launched := make(chan error, 1)
	go func() {
		launched <- defaultAppUpdateDependencies(vaultconfig.Env{}).launch(ctx, self,
			"-test.run=^TestPortableAppLaunchHelper$", "--", directory)
	}()
	select {
	case err := <-launched:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("app launch waited for the GUI process to exit")
	}
	require.NoError(t, waitForLaunchFixture(ctx, ready))
	cancel()
	_, err = os.Stat(done)
	require.ErrorIs(t, err, os.ErrNotExist, "canceling the update caller must not end the app process")
	require.NoError(t, os.WriteFile(release, nil, 0o600))
	cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	require.NoError(t, waitForLaunchFixture(cleanup, done))
}

func waitForLaunchFixture(ctx context.Context, path string) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestPortableAppLaunchHelper(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	directory := os.Args[len(os.Args)-1]
	if err := os.WriteFile(filepath.Join(directory, "ready"), nil, 0o600); err != nil {
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if waitForLaunchFixture(ctx, filepath.Join(directory, "release")) != nil {
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(directory, "done"), nil, 0o600); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
