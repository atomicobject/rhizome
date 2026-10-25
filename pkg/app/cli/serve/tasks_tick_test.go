package serve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInGitCheckoutFindsAParentRepository(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, "docs", "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		t.Fatal(err)
	}
	if inGitCheckout(vault) {
		t.Fatal("no repository yet")
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !inGitCheckout(vault) {
		t.Fatal("a vault nested in a checkout should ask for tasks")
	}
}

func TestRunTasksTickerRepeatsUntilCanceled(t *testing.T) {
	vault := t.TempDir()
	if err := os.Mkdir(filepath.Join(vault, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := make(chan struct{}, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runTasksTicker(ctx, vault, time.Millisecond, time.Millisecond, func(string) error {
			calls <- struct{}{}
			return nil
		})
	}()
	for i := 0; i < 3; i++ {
		select {
		case <-calls:
		case <-time.After(5 * time.Second):
			t.Fatalf("tick %d never came", i+1)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the ticker did not stop when its context ended")
	}
}
