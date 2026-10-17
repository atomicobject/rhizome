package ignore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStandardIgnorePatternsAgreeWithGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	cases := [][]string{
		{"/root-only.md"}, {"foo"}, {"vendor/"}, {"dir/*", "!dir/"},
		{"ignored/", "!ignored/"}, {"dir/", "!dir/file.txt"},
		{"*.md", "!keep.md"}, {"# comment", `\#literal`}, {"trailing   ", `escaped\ `}, {`literal\*.md`},
	}
	paths := []string{"root-only.md", "nested/root-only.md", "foo", "nested/foo", "vendor/file.go", "nested/vendor/file.go", "dir/file.txt", "ignored/file.txt", "keep.md", "other.md", "#literal", "trailing", "escaped ", "literal*.md", "literalX.md"}
	for _, patterns := range cases {
		t.Run(strings.Join(patterns, ";"), func(t *testing.T) {
			root := t.TempDir()
			cmd := exec.Command(git, "init", "--quiet", root)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git init: %s: %v", output, err)
			}
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(strings.Join(patterns, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			matcher := NewMatcher(patterns)
			for _, path := range paths {
				cmd := exec.Command(git, "-C", root, "check-ignore", "--no-index", "--quiet", "--", path)
				err := cmd.Run()
				want := err == nil
				if exit, ok := err.(*exec.ExitError); err != nil && (!ok || exit.ExitCode() != 1) {
					t.Fatalf("git check-ignore %q: %v", path, err)
				}
				if got := matcher.IsIgnored(path, false); got != want {
					t.Errorf("patterns %q path %q: matcher=%v git=%v", patterns, path, got, want)
				}
			}
		})
	}
}
