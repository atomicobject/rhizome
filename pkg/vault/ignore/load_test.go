package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUnifiedMatcher_NestedGitignoreOverridesParent(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.md\n"), 0o644); err != nil {
		t.Fatalf("write root .gitignore: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", ".gitignore"), []byte("!keep.md\n"), 0o644); err != nil {
		t.Fatalf("write nested .gitignore: %v", err)
	}

	matcher := LoadUnifiedMatcher(root, nil)
	if matcher == nil {
		t.Fatalf("expected matcher")
	}
	if !matcher.IsIgnored("keep.md", false) {
		t.Fatalf("expected root keep.md to be ignored")
	}
	if matcher.IsIgnored("sub/keep.md", false) {
		t.Fatalf("expected sub/keep.md to be unignored by nested .gitignore")
	}
	if !matcher.IsIgnored("sub/other.md", false) {
		t.Fatalf("expected sub/other.md to be ignored")
	}
}

func TestLoadUnifiedMatcher_IgnoredParentDirCannotBeReincluded(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatalf("write root .gitignore: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "ignored"), 0o755); err != nil {
		t.Fatalf("mkdir ignored: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored", ".gitignore"), []byte("!keep.md\n"), 0o644); err != nil {
		t.Fatalf("write nested .gitignore: %v", err)
	}

	matcher := LoadUnifiedMatcher(root, nil)
	if matcher == nil {
		t.Fatalf("expected matcher")
	}
	if !matcher.IsIgnored("ignored/keep.md", false) {
		t.Fatalf("expected ignored/keep.md to remain ignored")
	}
}

func TestLoadUnifiedMatcher_GitignoreScopedToDirectory(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatalf("mkdir b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", ".gitignore"), []byte("*.md\n"), 0o644); err != nil {
		t.Fatalf("write a/.gitignore: %v", err)
	}

	matcher := LoadUnifiedMatcher(root, nil)
	if matcher == nil {
		t.Fatalf("expected matcher")
	}
	if !matcher.IsIgnored("a/note.md", false) {
		t.Fatalf("expected a/note.md to be ignored by a/.gitignore")
	}
	if matcher.IsIgnored("b/note.md", false) {
		t.Fatalf("expected b/note.md to not be ignored by a/.gitignore")
	}
}
