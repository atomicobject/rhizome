//go:build !windows

package loginpath

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestShellPathReturnsWhenAProfileLeavesOutputOpen(t *testing.T) {
	shell := filepath.Join(t.TempDir(), "profile-shell")
	script := "#!/bin/sh\nsleep 30 &\nprintf '__RZM_LOGIN_PATH__/from/login'\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", shell)
	start := time.Now()
	got := shellPath()
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("shellPath waited %s for a background child's output", elapsed)
	}
	if got != "/from/login" {
		t.Fatalf("shellPath = %q, want %q", got, "/from/login")
	}
}
