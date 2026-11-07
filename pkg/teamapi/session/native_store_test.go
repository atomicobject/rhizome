//go:build linux || windows

package session

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

// TEAMAPI_NATIVE_STORE_TEST=1 exercises only synthetic, unique test entries.
func TestNativeStoreRoundTrip(t *testing.T) {
	if os.Getenv("TEAMAPI_NATIVE_STORE_TEST") != "1" {
		t.Skip("set TEAMAPI_NATIVE_STORE_TEST=1 to exercise the user's native store")
	}
	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	origin := fmt.Sprintf("https://native-store-%d-%d.invalid", os.Getpid(), time.Now().UnixNano())
	other := origin + ":4431"
	t.Cleanup(func() {
		if err := store.Delete(origin); err != nil {
			t.Errorf("remove synthetic test entry: %v", err)
		}
	})
	if _, err := store.Load(origin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load before Save = %v, want ErrNotFound", err)
	}
	if err := store.Delete(origin); err != nil {
		t.Fatalf("Delete absent entry: %v", err)
	}
	for _, record := range [][]byte{{0, 1, 127, 255}, []byte(`{"v":1,"access_token":"synthetic"}`)} {
		if err := store.Save(origin, record); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if got, err := store.Load(origin); err != nil || !bytes.Equal(got, record) {
			t.Fatalf("Load did not return the exact synthetic bytes: %v", err)
		}
	}
	if _, err := store.Load(other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("different origin Load = %v, want ErrNotFound", err)
	}
	if runtime.GOOS == "windows" {
		before, err := store.Load(origin)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Save(origin, make([]byte, 2561)); !errors.Is(err, ErrStoreUnavailable) {
			t.Fatalf("oversized Save = %v, want ErrStoreUnavailable", err)
		}
		if got, err := store.Load(origin); err != nil || !bytes.Equal(got, before) {
			t.Fatalf("oversized Save changed the existing record: %v", err)
		}
	}
	if err := store.Delete(origin); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(origin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}
}
