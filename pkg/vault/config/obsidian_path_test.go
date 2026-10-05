package config_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/config"
)

func TestConfigObsidianPath(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("AppData", base)
	wantDir := base
	if runtime.GOOS == "darwin" {
		wantDir = filepath.Join(base, "Library", "Application Support")
	}
	want := filepath.Join(wantDir, "obsidian", "obsidian.json")
	got, err := config.ObsidianFile()
	if err != nil || got != want {
		t.Fatalf("ObsidianFile() = %q, %v; want %q", got, err, want)
	}

	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("AppData", "")
	got, err = config.ObsidianFile()
	if got != "" || err == nil || err.Error() != config.UserConfigDirectoryNotFoundErrorMessage {
		t.Fatalf("ObsidianFile() = %q, %v; want empty path and config-dir error", got, err)
	}
}
