package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCasePolicyComparesUnderEachPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		pattern, rel    string
		exact, fold     bool
		equalExact      bool
		equalFoldPolicy bool
	}{
		{name: "same spelling", pattern: "MOCs/**/*.md", rel: "MOCs/riders.md", exact: true, fold: true, equalExact: false, equalFoldPolicy: false},
		{name: "folder case differs", pattern: "MOCs/**/*.md", rel: "mocs/riders.md", exact: false, fold: true},
		{name: "basename case differs", pattern: "docs/Readme.md", rel: "docs/readme.md", exact: false, fold: true, equalExact: false, equalFoldPolicy: true},
		{name: "character range folds", pattern: "notes/[A-Z]*.md", rel: "notes/alpha.md", exact: false, fold: true},
		{name: "different letters never match", pattern: "MOCs/**/*.md", rel: "docs/riders.md", exact: false, fold: false},
		{name: "composition is preserved", pattern: "café.md", rel: "café.md", exact: false, fold: false},
		{name: "bad pattern matches nothing", pattern: "notes/[", rel: "notes/[", exact: false, fold: false, equalExact: true, equalFoldPolicy: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CaseExact.Match(tt.pattern, tt.rel); got != tt.exact {
				t.Errorf("CaseExact.Match(%q, %q) = %v, want %v", tt.pattern, tt.rel, got, tt.exact)
			}
			if got := CaseFold.Match(tt.pattern, tt.rel); got != tt.fold {
				t.Errorf("CaseFold.Match(%q, %q) = %v, want %v", tt.pattern, tt.rel, got, tt.fold)
			}
			if got := CaseExact.Equal(tt.pattern, tt.rel); got != tt.equalExact {
				t.Errorf("CaseExact.Equal(%q, %q) = %v, want %v", tt.pattern, tt.rel, got, tt.equalExact)
			}
			if got := CaseFold.Equal(tt.pattern, tt.rel); got != tt.equalFoldPolicy {
				t.Errorf("CaseFold.Equal(%q, %q) = %v, want %v", tt.pattern, tt.rel, got, tt.equalFoldPolicy)
			}
		})
	}
	if CaseExact.Key("MOCs/Riders.md") != "MOCs/Riders.md" || CaseFold.Key("MOCs/Riders.md") != "mocs/riders.md" {
		t.Fatalf("Key must keep exact spelling and lowercase folded spelling")
	}
	if CaseExact.String() != "exact" || CaseFold.String() != "fold" {
		t.Fatalf("String names: %q %q", CaseExact.String(), CaseFold.String())
	}
}

func sameFileUnderSwappedCase(t *testing.T, dir, name string) bool {
	t.Helper()
	info, err := os.Lstat(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("Lstat(%s): %v", name, err)
	}
	other, err := os.Lstat(filepath.Join(dir, swapCase(name)))
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		t.Fatalf("Lstat(%s): %v", swapCase(name), err)
	}
	return os.SameFile(info, other)
}

func TestCasePolicyForProbesTheActualFilesystem(t *testing.T) {
	root := t.TempDir()
	if got := CasePolicyFor(root); got != CaseExact {
		t.Fatalf("empty root must compare exactly, got %s", got)
	}
	if got := CasePolicyFor(filepath.Join(root, "missing")); got != CaseExact {
		t.Fatalf("missing root must compare exactly, got %s", got)
	}
	if err := os.WriteFile(filepath.Join(root, "Probe.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := CaseExact
	if sameFileUnderSwappedCase(t, root, "Probe.md") {
		want = CaseFold
	}
	t.Logf("filesystem at %s compares %s", root, want)
	if got := CasePolicyFor(root); got != want {
		t.Fatalf("CasePolicyFor(%s) = %s, want %s from the filesystem", root, got, want)
	}
	vault, err := NewVaultPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := vault.Case(); got != want {
		t.Fatalf("VaultPaths.Case() = %s, want %s", got, want)
	}
	if want == CaseExact {
		if err := os.WriteFile(filepath.Join(root, "pROBE.MD"), []byte("y"), 0o644); err != nil {
			t.Fatal(err)
		}
		twin := t.TempDir()
		for _, name := range []string{"Probe.md", "pROBE.MD"} {
			if err := os.WriteFile(filepath.Join(twin, name), []byte(name), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if got := CasePolicyFor(twin); got != CaseExact {
			t.Fatalf("distinct twins must prove exact, got %s", got)
		}
	}
}

func TestExistingSpellingUsesDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Notes", "Sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Notes", "Sub", "Readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	vault, err := NewVaultPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	fold := sameFileUnderSwappedCase(t, filepath.Join(root, "Notes", "Sub"), "Readme.md")
	t.Logf("filesystem at %s folds case: %v", root, fold)

	if got, ok := vault.ExistingSpelling("Notes/Sub/Readme.md"); !ok || got != "Notes/Sub/Readme.md" {
		t.Fatalf("exact spelling = %q, %v; want itself", got, ok)
	}
	got, ok := vault.ExistingSpelling("notes/sub/README.MD")
	switch {
	case fold && (!ok || got != "Notes/Sub/Readme.md"):
		t.Fatalf("insensitive filesystem: spelling = %q, %v; want Notes/Sub/Readme.md", got, ok)
	case !fold && ok:
		t.Fatalf("sensitive filesystem: a differently cased spelling must not resolve, got %q", got)
	}
	for _, rel := range []string{"Notes/Sub/missing.md", "Elsewhere/Readme.md", "../Readme.md", ""} {
		if got, ok := vault.ExistingSpelling(RelPath(rel)); ok {
			t.Fatalf("ExistingSpelling(%q) = %q, want no spelling", rel, got)
		}
	}
	if !fold {
		if err := os.WriteFile(filepath.Join(root, "Notes", "Sub", "readme.md"), []byte("y"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, rel := range []string{"Notes/Sub/Readme.md", "Notes/Sub/readme.md"} {
			if got, ok := vault.ExistingSpelling(RelPath(rel)); !ok || got != RelPath(rel) {
				t.Fatalf("twin %q resolved to %q, %v", rel, got, ok)
			}
		}
		if got, ok := vault.ExistingSpelling("Notes/Sub/README.md"); ok {
			t.Fatalf("third spelling must stay unresolved among twins, got %q", got)
		}
	}
}

func TestCasePolicyForRelativeRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Probe.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if got, want := CasePolicyFor("."), CasePolicyFor(root); got != want {
		t.Fatalf("relative root policy = %s, absolute root policy = %s", got, want)
	}
}

func TestCasePolicyForDistinctHardlinkNames(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "Probe")
	if err := os.WriteFile(original, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if sameFileUnderSwappedCase(t, root, "Probe") {
		t.Skip("requires a case-sensitive filesystem")
	}
	if err := os.Link(original, filepath.Join(root, "pROBE")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if got := CasePolicyFor(root); got != CaseExact {
		t.Fatalf("two distinct directory entries must compare exactly, got %s", got)
	}
}
