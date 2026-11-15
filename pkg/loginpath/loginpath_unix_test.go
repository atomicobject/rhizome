//go:build !windows

package loginpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLookFindsLocalBinUnderBarePath(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "fake-agent-cli"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	got, err := Look("fake-agent-cli")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(bin, "fake-agent-cli"); got != want {
		t.Fatalf("Look = %q, want %q", got, want)
	}
}
