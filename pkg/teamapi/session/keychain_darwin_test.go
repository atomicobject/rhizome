//go:build darwin

package session

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestKeychainTimesOutWhenSecurityBlocks(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "security")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tool, timeout := securityTool, securityTimeout
	securityTool, securityTimeout = fake, 100*time.Millisecond
	t.Cleanup(func() { securityTool, securityTimeout = tool, timeout })

	start := time.Now()
	if _, err := (Keychain{}).Load("https://team-api.invalid"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Load = %v, want ErrStoreUnavailable", err)
	}
	if err := (Keychain{}).Save("https://team-api.invalid", []byte("x")); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Save = %v, want ErrStoreUnavailable", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("calls took %s; the timeout did not apply", elapsed)
	}
}

// Creates and deletes a throwaway keychain file, so it runs only when asked:
// TEAMAPI_KEYCHAIN_TEST=1 go test ./pkg/teamapi/session -run Keychain
func TestKeychainRoundTrip(t *testing.T) {
	if os.Getenv("TEAMAPI_KEYCHAIN_TEST") != "1" {
		t.Skip("set TEAMAPI_KEYCHAIN_TEST=1 to exercise a temporary macOS keychain")
	}
	path := filepath.Join(t.TempDir(), "teamapi-test.keychain-db")
	for _, args := range [][]string{{"create-keychain", "-p", "synthetic", path}, {"unlock-keychain", "-p", "synthetic", path}} {
		if out, err := exec.Command(securityTool, args...).CombinedOutput(); err != nil {
			t.Fatalf("security %s: %v %s", args[0], err, out)
		}
	}
	t.Cleanup(func() { _ = exec.Command(securityTool, "delete-keychain", path).Run() })

	k := Keychain{Path: path}
	const origin = "https://team-api.invalid"
	if _, err := k.Load(origin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load before Save = %v, want ErrNotFound", err)
	}
	for _, record := range [][]byte{[]byte(`{"v":1,"access_token":"synthetic-1"}`), []byte(`{"v":1,"access_token":"synthetic-2"}`)} {
		if err := k.Save(origin, record); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if got, err := k.Load(origin); err != nil || !bytes.Equal(got, record) {
			t.Fatalf("Load = %q, %v; want %q", got, err, record)
		}
	}
	if err := k.Delete(origin); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := k.Delete(origin); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if _, err := k.Load(origin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}

	// security writes to the default keychain when given a missing path.
	missing := Keychain{Path: filepath.Join(t.TempDir(), "missing.keychain-db")}
	if err := missing.Save(origin, []byte("x")); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Save to missing keychain = %v, want ErrStoreUnavailable", err)
	}
}
