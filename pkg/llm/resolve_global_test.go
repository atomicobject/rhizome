package llm

import (
	"github.com/atomicobject/rhizome/internal/testkit"
	"os"
	"path/filepath"
	"testing"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

func TestAPIKeyForProvider_FallsBackToGlobalConfig(t *testing.T) {
	env := testkit.Env(t)
	env.LookupEnv = os.LookupEnv

	home := t.TempDir()
	env.HomeDir = home

	cfgDir := filepath.Join(home, ".config", vaultconfig.RhizomeConfigDirectory)
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, vaultconfig.RhizomeConfigFile), []byte("env:\n  ANTHROPIC_API_KEY: from-global-config\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("ANTHROPIC_API_KEY", "")

	if got := APIKeyForProvider(env, "anthropic"); got != "from-global-config" {
		t.Fatalf("APIKeyForProvider() = %q, want %q", got, "from-global-config")
	}
}
