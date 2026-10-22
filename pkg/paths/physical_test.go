package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveAgainstJoinsWithoutCleaning(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	sep := string(filepath.Separator)
	abs := filepath.Join(t.TempDir(), "a.go")
	for _, tc := range []struct{ dir, p, want string }{
		{dir, "link/../a.go", dir + sep + "link" + sep + ".." + sep + "a.go"},
		{dir + sep, "a.go", dir + sep + "a.go"},
		{dir, abs, abs},
		{"", "a.go", "a.go"},
	} {
		if got := ResolveAgainst(tc.dir, tc.p); got != tc.want {
			t.Fatalf("ResolveAgainst(%q, %q) = %q, want %q", tc.dir, tc.p, got, tc.want)
		}
	}
}

func TestResolveAgainstGivesARootedWindowsPathTheDirectorysDrive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drives are a Windows path form")
	}
	for _, tc := range []struct{ p, want string }{
		{`/etc/hosts`, `C:\etc\hosts`},
		{`\etc\hosts`, `C:\etc\hosts`},
		{`D:\x\a.go`, `D:\x\a.go`},
		{`D:a.go`, `D:a.go`},
	} {
		if got := ResolveAgainst(`C:\repo`, tc.p); got != tc.want {
			t.Fatalf("ResolveAgainst(C:\\repo, %q) = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestRelPhysicalStrictFollowsALinkBeforeDotDot(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(root, "pkg"), filepath.Join(outside, "dir")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "dir"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "pkg"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	v, err := NewVaultPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	for _, tc := range []struct {
		p    string
		want RelPath
		err  error
	}{
		// link/../secret.go opens the outside file, though it cleans to secret.go.
		{root + sep + "link" + sep + ".." + sep + "secret.go", "", ErrOutsideVault},
		{root + sep + "link" + sep + "new.go", "", ErrOutsideVault},
		{root + sep + "pkg" + sep + ".." + sep + "secret.go", "secret.go", nil},
		{root + sep + "alias" + sep + "a.go", "pkg/a.go", nil},
		{root + sep + "gone" + sep + ".." + sep + "pkg" + sep + "a.go", "pkg/a.go", nil},
		{root + sep + ".." + sep + filepath.Base(root) + sep + "a.go", "a.go", nil},
		{filepath.Join(outside, "dir", "a.go"), "", ErrOutsideVault},
		{"pkg/a.go", "", ErrOutsideVault},
	} {
		got, err := v.RelPhysicalStrict(tc.p)
		if got != tc.want || err != tc.err {
			t.Fatalf("RelPhysicalStrict(%q) = %q, %v; want %q, %v", tc.p, got, err, tc.want, tc.err)
		}
	}
}
