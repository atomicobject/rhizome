package paths

import (
	"path/filepath"
	"testing"
)

func TestResolveNoteRefWithVaultPaths(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	vaultPaths, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths: %v", err)
	}

	ref, err := ResolveNoteRefWithVaultPaths(vaultPaths, "notes/a")
	if err != nil {
		t.Fatalf("ResolveNoteRefWithVaultPaths: %v", err)
	}
	if ref.Rel.String() != "notes/a.md" {
		t.Fatalf("Rel = %q, want %q", ref.Rel.String(), "notes/a.md")
	}
	wantAbs := filepath.Join(canonicalRoot, "notes", "a.md")
	if ref.Abs.String() != wantAbs {
		t.Fatalf("Abs = %q, want %q", ref.Abs.String(), wantAbs)
	}
}

func TestResolveNotePathRefWithVaultPathsPreservesAuthoredExtension(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	vaultPaths, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths: %v", err)
	}

	ref, err := ResolveNotePathRefWithVaultPaths(vaultPaths, "notes/a.html")
	if err != nil {
		t.Fatalf("ResolveNotePathRefWithVaultPaths: %v", err)
	}
	if ref.Rel.String() != "notes/a.html" {
		t.Fatalf("Rel = %q, want %q", ref.Rel.String(), "notes/a.html")
	}
	wantAbs := filepath.Join(canonicalRoot, "notes", "a.html")
	if ref.Abs.String() != wantAbs {
		t.Fatalf("Abs = %q, want %q", ref.Abs.String(), wantAbs)
	}
}

func TestAbsNotePathRejectsNonCanonicalNotePath(t *testing.T) {
	root := t.TempDir()
	vaultPaths, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths: %v", err)
	}
	for _, path := range []NotePath{"/outside/note.html", "../outside/note.html", "C:\\outside\\note.html"} {
		if _, err := vaultPaths.AbsNotePath(path); err == nil {
			t.Fatalf("AbsNotePath(%q) succeeded", path)
		}
	}
	if _, err := vaultPaths.AbsNotePath("notes/decision.html"); err != nil {
		t.Fatalf("AbsNotePath valid HTML path: %v", err)
	}
}

func TestResolveCodeRefWithVaultPaths(t *testing.T) {
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	vaultPaths, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths: %v", err)
	}

	ref, err := ResolveCodeRefWithVaultPaths(vaultPaths, "src/main.go")
	if err != nil {
		t.Fatalf("ResolveCodeRefWithVaultPaths: %v", err)
	}
	if ref.Rel.String() != "src/main.go" {
		t.Fatalf("Rel = %q, want %q", ref.Rel.String(), "src/main.go")
	}
	wantAbs := filepath.Join(canonicalRoot, "src", "main.go")
	if ref.Abs.String() != wantAbs {
		t.Fatalf("Abs = %q, want %q", ref.Abs.String(), wantAbs)
	}
}

func TestResolveRefOutsideVault(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	vaultPaths, err := NewVaultPaths(root)
	if err != nil {
		t.Fatalf("NewVaultPaths: %v", err)
	}

	_, err = ResolveNoteRefWithVaultPaths(vaultPaths, filepath.Join(other, "outside.md"))
	if err != ErrOutsideVault {
		t.Fatalf("expected ErrOutsideVault, got %v", err)
	}

	_, err = ResolveCodeRefWithVaultPaths(vaultPaths, filepath.Join(other, "outside.go"))
	if err != ErrOutsideVault {
		t.Fatalf("expected ErrOutsideVault, got %v", err)
	}
}
