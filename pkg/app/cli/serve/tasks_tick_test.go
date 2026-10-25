package serve

import (
	"os"
	"path/filepath"
	"testing"
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
