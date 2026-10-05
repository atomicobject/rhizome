package llm

import (
	"os"
	"path/filepath"
	"testing"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

func TestAPIKeyForProvider_FallsBackToGlobalConfig(t *testing.T) {
	home := t.TempDir()
	origHome := vaultconfig.UserHomeDirectory
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	t.Cleanup(func() { vaultconfig.UserHomeDirectory = origHome })

	cfgDir := filepath.Join(home, ".config", vaultconfig.RhizomeConfigDirectory)
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, vaultconfig.RhizomeConfigFile), []byte("env:\n  ANTHROPIC_API_KEY: from-global-config\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "")

	if got := APIKeyForProvider("anthropic"); got != "from-global-config" {
		t.Fatalf("APIKeyForProvider() = %q, want %q", got, "from-global-config")
	}
}
