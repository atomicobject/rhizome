package indexing

import (
	"context"
	"io"
	"path/filepath"
	"testing"
)

func TestWithCommandIndexLockGuardsWriterAndReleases(t *testing.T) {
	vaultPath := t.TempDir()
	acquired := false
	released := false
	ran := false

	err := withCommandIndexLockUsing(context.Background(), vaultPath, io.Discard,
		func(_ context.Context, lockPath string, requestPriority, debug bool, _ io.Writer) (func() error, error) {
			if lockPath != filepath.Join(vaultPath, ".rhizome", "index.lock") {
				t.Fatalf("unexpected lock path %q", lockPath)
			}
			if !requestPriority || debug {
				t.Fatalf("expected interactive priority without debug")
			}
			acquired = true
			return func() error {
				released = true
				return nil
			}, nil
		},
		func() error {
			if !acquired {
				t.Fatal("writer ran before lock acquisition")
			}
			ran = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("with command index lock: %v", err)
	}
	if !ran || !released {
		t.Fatalf("expected writer and release; ran=%v released=%v", ran, released)
	}
}
