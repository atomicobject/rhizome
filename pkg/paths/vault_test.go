package paths

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultPathsRelStrict(t *testing.T) {
	root := t.TempDir()
	v, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths error: %v", err)
	}

	absInside := filepath.Join(root, "notes", "a.md")
	rel, err := v.RelStrict(absInside)
	if err != nil {
		t.Fatalf("RelStrict error: %v", err)
	}
	if rel != "notes/a.md" {
		t.Fatalf("RelStrict(%q) = %q, want %q", absInside, rel, "notes/a.md")
	}

	absOutside := filepath.Join(root, "..", "outside.md")
	if _, err := v.RelStrict(absOutside); err != ErrOutsideVault {
		t.Fatalf("RelStrict outside vault error = %v, want %v", err, ErrOutsideVault)
	}

	if _, err := v.RelStrict("../outside.md"); err != ErrOutsideVault {
		t.Fatalf("RelStrict escaped rel error = %v, want %v", err, ErrOutsideVault)
	}

	if _, err := v.RelStrict("C:\\temp\\a.md"); err != ErrOutsideVault {
		t.Fatalf("RelStrict windows abs error = %v, want %v", err, ErrOutsideVault)
	}
}

func TestVaultPathsRequireCurrentWorkingDirectoryAtRoot(t *testing.T) {
	root := t.TempDir()
	v, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths error: %v", err)
	}

	t.Chdir(root)
	if err := v.RequireCurrentWorkingDirectoryAtRoot(); err != nil {
		t.Fatalf("RequireCurrentWorkingDirectoryAtRoot at root: %v", err)
	}

	subdir := filepath.Join(root, "nested")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	t.Chdir(subdir)
	if err := v.RequireCurrentWorkingDirectoryAtRoot(); !errors.Is(err, ErrWorkingDirectoryNotVaultRoot) {
		t.Fatalf("RequireCurrentWorkingDirectoryAtRoot below root error = %v, want %v", err, ErrWorkingDirectoryNotVaultRoot)
	}
}

func TestCleanRelPathRejectsAbsoluteAndTraversalPaths(t *testing.T) {
	for _, input := range []string{"/outside/note.html", "../outside/note.html", "..", "C:\\outside\\note.html", "C:/abs/path"} {
		if _, err := CleanRelPath(input); err != ErrOutsideVault {
			t.Fatalf("CleanRelPath(%q) error = %v, want ErrOutsideVault", input, err)
		}
	}

	got, err := CleanRelPath("./notes/decision.html")
	if err != nil {
		t.Fatalf("CleanRelPath valid path: %v", err)
	}
	if got != "notes/decision.html" {
		t.Fatalf("CleanRelPath = %q, want notes/decision.html", got)
	}
	got, err = CleanRelPath("foo/bar")
	if err != nil || got != "foo/bar" {
		t.Fatalf("CleanRelPath(foo/bar) = %q, %v", got, err)
	}
}

func TestVaultPathsRelNoteStrict(t *testing.T) {
	root := t.TempDir()
	v, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths error: %v", err)
	}

	rel, err := v.RelNoteStrict("notes/a")
	if err != nil {
		t.Fatalf("RelNoteStrict error: %v", err)
	}
	if rel != "notes/a.md" {
		t.Fatalf("RelNoteStrict = %q, want %q", rel, "notes/a.md")
	}
}

func TestVaultPathsRelNotePathStrictDoesNotInferMarkdownSuffix(t *testing.T) {
	root := t.TempDir()
	v, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths error: %v", err)
	}

	rel, err := v.RelNotePathStrict("notes/a.HTML")
	if err != nil {
		t.Fatalf("RelNotePathStrict error: %v", err)
	}
	if rel != "notes/a.HTML" {
		t.Fatalf("RelNotePathStrict = %q, want %q", rel, "notes/a.HTML")
	}
}

func TestVaultPathsRelCodeStrict(t *testing.T) {
	root := t.TempDir()
	v, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths error: %v", err)
	}

	rel, err := v.RelCodeStrict("src/main.go")
	if err != nil {
		t.Fatalf("RelCodeStrict error: %v", err)
	}
	if rel != "src/main.go" {
		t.Fatalf("RelCodeStrict = %q, want %q", rel, "src/main.go")
	}
}
