package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOrdinaryDOSPathKeepsOtherDeviceNamespaces(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`C:\repo`, `C:\repo`},
		{`\\?\C:\repo`, `C:\repo`},
		{`\\?\c:\repo`, `c:\repo`},
		{`\\?\UNC\server\share\repo`, `\\server\share\repo`},
		{`\\?\unc\server\share\repo`, `\\server\share\repo`},
		{`\\?\GLOBALROOT\Device\HarddiskVolume1\repo`, `\\?\GLOBALROOT\Device\HarddiskVolume1\repo`},
		{`\\?\Volume{00000000-0000-0000-0000-000000000000}\repo`, `\\?\Volume{00000000-0000-0000-0000-000000000000}\repo`},
		{`\\.\C:\repo`, `\\.\C:\repo`},
		{`\\?\C:repo`, `\\?\C:repo`},
		{`\\?\1:\repo`, `\\?\1:\repo`},
	} {
		if got := ordinaryDOSPath(tc.input); got != tc.want {
			t.Errorf("ordinaryDOSPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCanonicalExistingPathRequiresExistingFilesystemIdentity(t *testing.T) {
	dir := t.TempDir()
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := CanonicalExistingPath(dir)
	if err != nil || got.String() != want {
		t.Fatalf("CanonicalExistingPath(%q) = %q, %v; want %q", dir, got, err, want)
	}
	if _, err := CanonicalExistingPath(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing path error = %v, want os.ErrNotExist", err)
	}
}

func TestVerbatimDOSPathPreservesNamesWithoutRewritingDeviceNamespaces(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`C:\repo.`, `\\?\C:\repo.`},
		{`C:\parent \repo`, `\\?\C:\parent \repo`},
		{`\\server\share\repo.`, `\\?\UNC\server\share\repo.`},
		{`\\?\GLOBALROOT\Device\repo.`, `\\?\GLOBALROOT\Device\repo.`},
		{`\\?\Volume{00000000-0000-0000-0000-000000000000}\repo.`, `\\?\Volume{00000000-0000-0000-0000-000000000000}\repo.`},
		{`\\.\C:\repo.`, `\\.\C:\repo.`},
	} {
		if got := verbatimDOSPath(tc.input); got != tc.want {
			t.Errorf("verbatimDOSPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
