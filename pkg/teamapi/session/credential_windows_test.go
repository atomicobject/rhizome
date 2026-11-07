//go:build windows

package session

import (
	"errors"
	"strings"
	"testing"
)

func TestCredentialManagerRejectsOversizedRecord(t *testing.T) {
	err := (CredentialManager{}).Save("https://team.example.invalid", make([]byte, 2561))
	if !errors.Is(err, ErrStoreUnavailable) || !strings.Contains(err.Error(), "2560-byte limit") {
		t.Fatalf("oversized Save = %v, want the native limit without a write", err)
	}
}

func TestCredentialManagerRejectsNoncanonicalOrigins(t *testing.T) {
	store := CredentialManager{}
	for _, origin := range []string{"", "https://team.example.invalid/path", "https://TEAM.example.invalid", "http://remote.invalid"} {
		if _, err := store.Load(origin); !errors.Is(err, ErrStoreUnavailable) {
			t.Fatalf("Load(%q) = %v", origin, err)
		}
		if err := store.Save(origin, []byte("synthetic")); !errors.Is(err, ErrStoreUnavailable) {
			t.Fatalf("Save(%q) = %v", origin, err)
		}
		if err := store.Delete(origin); !errors.Is(err, ErrStoreUnavailable) {
			t.Fatalf("Delete(%q) = %v", origin, err)
		}
	}
}
