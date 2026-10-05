package embeddings

import (
	"os"
	"path/filepath"
	"testing"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

func TestResolveAPIKey_FallsBackToGlobalConfig(t *testing.T) {
	home := t.TempDir()
	origHome := vaultconfig.UserHomeDirectory
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	t.Cleanup(func() { vaultconfig.UserHomeDirectory = origHome })

	cfgDir := filepath.Join(home, ".config", vaultconfig.RhizomeConfigDirectory)
	mustMkdirAll(t, cfgDir)
	mustWriteFile(t, filepath.Join(cfgDir, vaultconfig.RhizomeConfigFile), []byte("env:\n  RHIZOME_OPENAI_API_KEY: from-global-config\n"))

	t.Setenv("RHIZOME_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ATOMIC_RHIZOME_KEY", "")

	if got := ResolveAPIKey(""); got != "from-global-config" {
		t.Fatalf("ResolveAPIKey() = %q, want %q", got, "from-global-config")
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
