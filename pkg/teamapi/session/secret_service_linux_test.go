//go:build linux

package session

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeSecretTool(t *testing.T, script string) SecretService {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret-tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return SecretService{tool: path, timeout: time.Second}
}

func TestSecretServiceMissingAndLocked(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(fmt.Sprint(locked), func(t *testing.T) {
			search := ""
			if locked {
				search = "echo '[/synthetic-item]'; echo 'label = Rhizome team session'"
			}
			s := fakeSecretTool(t, "case $1 in\nlookup|clear) exit 1;;\nsearch) "+search+"\nexit 0;;\nesac\n")
			_, err := s.Load("https://synthetic.invalid")
			want := ErrNotFound
			if locked {
				want = ErrStoreUnavailable
			}
			if !errors.Is(err, want) {
				t.Fatalf("Load = %v, want %v", err, want)
			}
			err = s.Delete("https://synthetic.invalid")
			if locked && !errors.Is(err, ErrStoreUnavailable) || !locked && err != nil {
				t.Fatalf("Delete = %v, locked=%v", err, locked)
			}
			if locked && !errors.Is(s.Save("https://synthetic.invalid", []byte("synthetic")), ErrStoreUnavailable) {
				t.Fatal("Save accepted a locked matching item")
			}
		})
	}
}

func TestSecretServiceDoesNotExposeHelperOutput(t *testing.T) {
	s := fakeSecretTool(t, "echo synthetic-private-output; echo synthetic-private-output >&2; exit 1\n")
	_, err := s.Load("https://synthetic.invalid")
	if !errors.Is(err, ErrStoreUnavailable) || strings.Contains(err.Error(), "synthetic-private-output") {
		t.Fatalf("Load error = %v", err)
	}
}

func TestSecretServiceTimeout(t *testing.T) {
	s := fakeSecretTool(t, "exec sleep 5\n")
	s.timeout = 30 * time.Millisecond
	_, err := s.Load("https://synthetic.invalid")
	if !errors.Is(err, ErrStoreUnavailable) || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("Load = %v", err)
	}
}

func TestSecretServicePrivateTransportAndTrustedEnvironment(t *testing.T) {
	const record = "synthetic-session-token"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(record))
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "/tmp/malicious-home")
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/malicious-runtime")
	t.Setenv("SECRET_SERVICE_BUS_NAME", "malicious.service")
	t.Setenv("SECRET_BACKEND", "file")
	t.Setenv("SNAP_NAME", "malicious")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/tmp/malicious")
	t.Setenv("LD_PRELOAD", "/tmp/malicious.so")
	s := fakeSecretTool(t, fmt.Sprintf(`
[ "$HOME" = %q ]
[ "$XDG_RUNTIME_DIR" = %q ]
[ "$SECRET_SERVICE_BUS_NAME" = org.freedesktop.secrets ]
[ "$SECRET_BACKEND" = service ]
[ -z "${SNAP_NAME:-}${LD_PRELOAD:-}" ]
[ "$DBUS_SESSION_BUS_ADDRESS" = "unix:path=$XDG_RUNTIME_DIR/bus" ]
case "$*" in *%s*) exit 2;; esac
case "$1" in
search) exit 0;;
store) [ "$(cat)" = %s ];;
lookup) printf '%%s' %s;;
esac
`, account.HomeDir, filepath.Join("/run/user", account.Uid), encoded, encoded, encoded))
	if err := s.Save("https://synthetic.invalid", []byte(record)); err != nil {
		t.Fatal(err)
	}
}

func TestSecretServiceRejectsMalformedAndUnconfirmedWrites(t *testing.T) {
	s := fakeSecretTool(t, "case $1 in lookup) echo not-base64!;; esac\n")
	if _, err := s.Load("https://synthetic.invalid"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("malformed Load = %v", err)
	}
	s = fakeSecretTool(t, "case $1 in lookup) echo b3RoZXI;; esac\n")
	if err := s.Save("https://synthetic.invalid", []byte("synthetic")); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("unconfirmed Save = %v", err)
	}
}

// Uses only a unique synthetic item in the real keyring; never locks or reads
// unrelated items. TEAMAPI_SECRET_SERVICE_TEST=1 enables native acceptance.
func TestSecretServiceNativeRoundTrip(t *testing.T) {
	if os.Getenv("TEAMAPI_SECRET_SERVICE_TEST") != "1" {
		t.Skip("set TEAMAPI_SECRET_SERVICE_TEST=1 for native Secret Service acceptance")
	}
	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	s := store.(SecretService)
	origin := fmt.Sprintf("https://rhizome-synthetic-%d.invalid", time.Now().UnixNano())
	t.Cleanup(func() {
		if err := s.Delete(origin); err != nil {
			t.Errorf("synthetic item cleanup: %v", err)
		}
	})
	if _, err := s.Load(origin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("initial Load = %v", err)
	}
	for _, record := range [][]byte{[]byte("synthetic-1"), []byte("synthetic-2")} {
		if err := s.Save(origin, record); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Load(origin + ":8443"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("record leaked across origins: %v", err)
		}
		got, err := (SecretService{}).Load(origin)
		if err != nil || !bytes.Equal(got, record) {
			t.Fatal("independent store did not read the saved synthetic record")
		}
	}
	for range 2 {
		if err := s.Delete(origin); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Load(origin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted Load = %v", err)
	}
}

func TestSecretServiceNativeUnavailableBus(t *testing.T) {
	if os.Getenv("TEAMAPI_SECRET_SERVICE_TEST") != "1" {
		t.Skip("set TEAMAPI_SECRET_SERVICE_TEST=1 for native Secret Service acceptance")
	}
	s := fakeSecretTool(t, "export DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent-rhizome-synthetic-bus\nexec /usr/bin/secret-tool \"$@\"\n")
	if _, err := s.Load("https://synthetic.invalid"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("unavailable native service = %v", err)
	}
}
