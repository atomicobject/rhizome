package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveValue_PrefersProcessEnvOverGlobalConfig(t *testing.T) {
	home := t.TempDir()
	origHome := UserHomeDirectory
	UserHomeDirectory = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDirectory = origHome })

	cfgDir := filepath.Join(home, ".config", RhizomeConfigDirectory)
	requireMkdirAll(t, cfgDir)
	requireWriteFile(t, filepath.Join(cfgDir, RhizomeConfigFile), []byte("env:\n  OPENAI_API_KEY: from-config\n"))

	t.Setenv("OPENAI_API_KEY", "from-env")

	if got := ResolveValue("OPENAI_API_KEY"); got != "from-env" {
		t.Fatalf("ResolveValue() = %q, want %q", got, "from-env")
	}
}

func TestResolveValue_FallsBackToGlobalConfig(t *testing.T) {
	home := t.TempDir()
	origHome := UserHomeDirectory
	UserHomeDirectory = func() (string, error) { return home, nil }
	t.Cleanup(func() { UserHomeDirectory = origHome })

	cfgDir := filepath.Join(home, ".config", RhizomeConfigDirectory)
	requireMkdirAll(t, cfgDir)
	requireWriteFile(t, filepath.Join(cfgDir, RhizomeConfigFile), []byte("env:\n  OPENAI_API_KEY: from-config\n  ATOMIC_RHIZOME_KEY: from-team-config\n"))

	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ATOMIC_RHIZOME_KEY", "")

	if got := ResolveValue("OPENAI_API_KEY"); got != "from-config" {
		t.Fatalf("ResolveValue(OPENAI_API_KEY) = %q, want %q", got, "from-config")
	}
	if got := ResolveValue("ATOMIC_RHIZOME_KEY"); got != "from-team-config" {
		t.Fatalf("ResolveValue(ATOMIC_RHIZOME_KEY) = %q, want %q", got, "from-team-config")
	}
}

func requireMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func requireWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
