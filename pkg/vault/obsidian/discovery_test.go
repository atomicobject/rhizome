package obsidian

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
)

func TestDiscoverFiles_Classical(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "A.md"))
	mkfile(t, filepath.Join(root, "nested", "B.md"))
	mkfile(t, filepath.Join(root, "nested", ".hidden.md"))
	mkfile(t, filepath.Join(root, "note.txt"))

	files, err := DiscoverFiles(VaultDefinition{Path: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(files)
	want := []string{"A.md", "nested/B.md"}
	if !slices.Equal(files, want) {
		t.Fatalf("expected %v, got %v", want, files)
	}
}

func TestDiscoverFiles_ClassicalDirExcludeKeepsOnlySystemContext(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "public.md"))
	mkfile(t, filepath.Join(root, "private", "ordinary.md"))
	mkfile(t, filepath.Join(root, "private", "CONTEXT.md"))

	files, err := DiscoverFiles(VaultDefinition{
		Path:     root,
		Excludes: []string{"private/"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(files)
	want := []string{"private/CONTEXT.md", "public.md"}
	if !slices.Equal(files, want) {
		t.Fatalf("expected %v, got %v", want, files)
	}
}

func TestDiscoverFiles_Collection(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "docs", "A.md"))
	mkfile(t, filepath.Join(root, "src", "notes", "B.md"))
	mkfile(t, filepath.Join(root, "src", "notes", "skip.md"))

	files, err := DiscoverFiles(VaultDefinition{
		Root:     root,
		Includes: []string{"docs/**/*.md", "src/**/*.md"},
		Excludes: []string{"**/skip.md"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(files)
	want := []string{"docs/A.md", "src/notes/B.md"}
	if !slices.Equal(files, want) {
		t.Fatalf("expected %v, got %v", want, files)
	}
}

func TestDiscoverFiles_CollectionAlwaysIncludesContextWithinIgnoreBoundaries(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "docs", "Note.md"))
	mkfile(t, filepath.Join(root, "CONTEXT.md"))
	mkfile(t, filepath.Join(root, "src", "service", "CONTEXT.md"))
	mkfile(t, filepath.Join(root, "src", "service", "context.md"))
	mkfile(t, filepath.Join(root, "private", "CONTEXT.md"))
	mkfile(t, filepath.Join(root, "hard-hidden", "CONTEXT.md"))
	mkfile(t, filepath.Join(root, "git-hidden", "CONTEXT.md"))
	mkfile(t, filepath.Join(root, "node_modules", "pkg", "CONTEXT.md"))
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("git-hidden/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".rhizome"), 0o755); err != nil {
		t.Fatalf("mkdir .rhizome: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".rhizome", "ignore"), []byte("hard-hidden/\n"), 0o644); err != nil {
		t.Fatalf("write .rhizome/ignore: %v", err)
	}

	files, err := DiscoverFiles(VaultDefinition{
		Root:     root,
		Includes: []string{"docs/**/*.md"},
		Excludes: []string{"private/**"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(files)
	want := []string{"CONTEXT.md", "docs/Note.md", "private/CONTEXT.md", "src/service/CONTEXT.md"}
	if !slices.Equal(files, want) {
		t.Fatalf("expected %v, got %v", want, files)
	}
}

func TestDiscoverFiles_CollectionRootOnly(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "A.md"))
	mkfile(t, filepath.Join(root, "nested", "B.md"))
	mkfile(t, filepath.Join(root, "nested", ".hidden.md"))
	mkfile(t, filepath.Join(root, "note.txt"))

	// Collection with Root only - uses default **/*.md pattern
	files, err := DiscoverFiles(VaultDefinition{Root: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(files)
	want := []string{"A.md", "nested/B.md"}
	if !slices.Equal(files, want) {
		t.Fatalf("expected %v, got %v", want, files)
	}
}

func TestDiscoverFiles_PreservesMarkdownExtensionCasing(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"one.md", "two.mD", "three.Md", "four.MD"} {
		mkfile(t, filepath.Join(root, "docs", name))
	}
	mkfile(t, filepath.Join(root, ".hidden.MD"))
	mkfile(t, filepath.Join(root, ".hidden", "nested.Md"))

	t.Run("classic", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{Path: root})
		if err != nil {
			t.Fatalf("DiscoverFiles: %v", err)
		}
		slices.Sort(files)
		want := []string{"docs/four.MD", "docs/one.md", "docs/three.Md", "docs/two.mD"}
		if !slices.Equal(files, want) {
			t.Fatalf("DiscoverFiles = %v, want %v", files, want)
		}
	})

	t.Run("collection defaults", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{Root: root})
		if err != nil {
			t.Fatalf("DiscoverFiles: %v", err)
		}
		slices.Sort(files)
		want := []string{"docs/four.MD", "docs/one.md", "docs/three.Md", "docs/two.mD"}
		if !slices.Equal(files, want) {
			t.Fatalf("DiscoverFiles = %v, want %v", files, want)
		}
	})

	t.Run("collection literal extension", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{Root: root, Includes: []string{"docs/**/*.md"}})
		if err != nil {
			t.Fatalf("DiscoverFiles: %v", err)
		}
		slices.Sort(files)
		want := []string{"docs/four.MD", "docs/one.md", "docs/three.Md", "docs/two.mD"}
		if !slices.Equal(files, want) {
			t.Fatalf("DiscoverFiles = %v, want %v", files, want)
		}
	})
}

func TestNotePathMatchesIncludes_MarkdownExtensionCasing(t *testing.T) {
	cfg := VaultDefinition{Root: "/vault", Includes: []string{"docs/Exact.md"}}
	for _, path := range []string{"docs/Exact.md", "docs/Exact.mD", "docs/Exact.Md", "docs/Exact.MD"} {
		if !NotePathMatchesIncludes(cfg, path) {
			t.Errorf("NotePathMatchesIncludes(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"Docs/Exact.MD", "docs/exact.MD", "docs/Exact.txt"} {
		if NotePathMatchesIncludes(cfg, path) {
			t.Errorf("NotePathMatchesIncludes(%q) = true, want false", path)
		}
	}
}

func mkfile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("# test"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestVaultDefinition_IsCollection(t *testing.T) {
	tests := []struct {
		name string
		def  VaultDefinition
		want bool
	}{
		{"classic vault with Path", VaultDefinition{Path: "/some/path"}, false},
		{"collection with Root and Includes", VaultDefinition{Root: "/root", Includes: []string{"docs/**/*.md"}}, true},
		{"collection with Root only (default includes)", VaultDefinition{Root: "/root"}, true},
		{"empty definition", VaultDefinition{}, false},
		{"Path and Root both set prefers Root", VaultDefinition{Path: "/path", Root: "/root"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.def.IsCollection()
			if got != tc.want {
				t.Errorf("IsCollection() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDiscoverFiles_DefaultIgnores(t *testing.T) {
	root := t.TempDir()

	// Create files in default ignore patterns
	mkfile(t, filepath.Join(root, "A.md"))
	mkfile(t, filepath.Join(root, "node_modules", "pkg", "B.md"))
	mkfile(t, filepath.Join(root, "vendor", "C.md"))
	mkfile(t, filepath.Join(root, "dist", "D.md"))
	mkfile(t, filepath.Join(root, ".git", "E.md"))
	mkfile(t, filepath.Join(root, "docs", "F.md"))

	t.Run("classic vault ignores default patterns", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{Path: root})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slices.Sort(files)
		want := []string{"A.md", "docs/F.md"}
		if !slices.Equal(files, want) {
			t.Fatalf("expected %v, got %v", want, files)
		}
	})

	t.Run("collection vault ignores default patterns", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{Root: root})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slices.Sort(files)
		want := []string{"A.md", "docs/F.md"}
		if !slices.Equal(files, want) {
			t.Fatalf("expected %v, got %v", want, files)
		}
	})
}

func TestDiscoverFiles_ObsidianIgnore(t *testing.T) {
	root := t.TempDir()

	mkfile(t, filepath.Join(root, "A.md"))
	mkfile(t, filepath.Join(root, "ignored", "B.md"))
	mkfile(t, filepath.Join(root, "docs", "C.md"))

	// Create .rhizome/ignore
	ignoreContent := "ignored/\n"
	ignorePath := filepath.Join(root, ".rhizome/ignore")
	if err := os.MkdirAll(filepath.Dir(ignorePath), 0o755); err != nil {
		t.Fatalf("mkdir .rhizome: %v", err)
	}
	if err := os.WriteFile(ignorePath, []byte(ignoreContent), 0o644); err != nil {
		t.Fatalf("write .rhizome/ignore: %v", err)
	}

	t.Run("classic vault respects .rhizome/ignore", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{Path: root})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slices.Sort(files)
		want := []string{"A.md", "docs/C.md"}
		if !slices.Equal(files, want) {
			t.Fatalf("expected %v, got %v", want, files)
		}
	})

	t.Run("collection vault respects .rhizome/ignore", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{Root: root})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slices.Sort(files)
		want := []string{"A.md", "docs/C.md"}
		if !slices.Equal(files, want) {
			t.Fatalf("expected %v, got %v", want, files)
		}
	})
}

func TestDiscoverFiles_GitignoreAndRhizomeOverride(t *testing.T) {
	root := t.TempDir()

	mkfile(t, filepath.Join(root, "keep", "A.md"))
	mkfile(t, filepath.Join(root, "ignored", "B.md"))

	// Root .gitignore ignores the directory.
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	t.Run("gitignore excludes by default", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{Path: root})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slices.Sort(files)
		want := []string{"keep/A.md"}
		if !slices.Equal(files, want) {
			t.Fatalf("expected %v, got %v", want, files)
		}
	})

	t.Run(".rhizome/ignore can re-include via negation", func(t *testing.T) {
		ignorePath := filepath.Join(root, ".rhizome/ignore")
		if err := os.MkdirAll(filepath.Dir(ignorePath), 0o755); err != nil {
			t.Fatalf("mkdir .rhizome: %v", err)
		}
		// Unignore the directory itself, then explicitly unignore its contents.
		if err := os.WriteFile(ignorePath, []byte("!ignored/\n!ignored/**\n"), 0o644); err != nil {
			t.Fatalf("write .rhizome/ignore: %v", err)
		}

		files, err := DiscoverFiles(VaultDefinition{Path: root})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slices.Sort(files)
		want := []string{"ignored/B.md", "keep/A.md"}
		if !slices.Equal(files, want) {
			t.Fatalf("expected %v, got %v", want, files)
		}
	})
}

func TestDiscoverFiles_NestedGitignoreOverride(t *testing.T) {
	root := t.TempDir()

	mkfile(t, filepath.Join(root, "keep.md"))
	mkfile(t, filepath.Join(root, "sub", "keep.md"))
	mkfile(t, filepath.Join(root, "sub", "other.md"))

	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.md\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", ".gitignore"), []byte("!keep.md\n"), 0o644); err != nil {
		t.Fatalf("write nested .gitignore: %v", err)
	}

	files, err := DiscoverFiles(VaultDefinition{Path: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(files)
	want := []string{"sub/keep.md"}
	if !slices.Equal(files, want) {
		t.Fatalf("expected %v, got %v", want, files)
	}
}

func TestDiscoverFiles_ExcludesGlob(t *testing.T) {
	root := t.TempDir()

	mkfile(t, filepath.Join(root, "docs", "public", "A.md"))
	mkfile(t, filepath.Join(root, "docs", "private", "B.md"))
	mkfile(t, filepath.Join(root, "docs", "secret.md"))
	mkfile(t, filepath.Join(root, "readme.md"))

	t.Run("classic vault applies Excludes as globs", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{
			Path:     root,
			Excludes: []string{"**/private/**", "**/secret.md"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slices.Sort(files)
		want := []string{"docs/public/A.md", "readme.md"}
		if !slices.Equal(files, want) {
			t.Fatalf("expected %v, got %v", want, files)
		}
	})

	t.Run("collection vault applies Excludes as globs", func(t *testing.T) {
		files, err := DiscoverFiles(VaultDefinition{
			Root:     root,
			Includes: []string{"docs/**/*.md"},
			Excludes: []string{"**/private/**", "**/secret.md"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		slices.Sort(files)
		want := []string{"docs/public/A.md"}
		if !slices.Equal(files, want) {
			t.Fatalf("expected %v, got %v", want, files)
		}
	})
}

// readDirRecorder records every directory read so tests can assert pruning
// without timing.
type readDirRecorder struct {
	fs.FS
	dirs []string
}

func (r *readDirRecorder) ReadDir(name string) ([]fs.DirEntry, error) {
	r.dirs = append(r.dirs, name)
	return fs.ReadDir(r.FS, name)
}

func TestDiscoverFiles_CollectionNeverReadsIgnoredDirectories(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "docs", "A.md"))
	mkfile(t, filepath.Join(root, "README.md"))
	for i := 0; i < 50; i++ {
		mkfile(t, filepath.Join(root, "node_modules", "pkg"+strconv.Itoa(i), "lib", "index.js"))
		mkfile(t, filepath.Join(root, "build", "out"+strconv.Itoa(i), "page.md"))
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}

	recorder := &readDirRecorder{}
	discoveryFS = func(dir string) fs.FS {
		recorder.FS = os.DirFS(dir)
		return recorder
	}
	t.Cleanup(func() { discoveryFS = os.DirFS })

	files, err := DiscoverFiles(VaultDefinition{Root: root, Includes: []string{"**/*.md"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	slices.Sort(files)
	want := []string{"README.md", "docs/A.md"}
	if !slices.Equal(files, want) {
		t.Fatalf("expected %v, got %v", want, files)
	}
	for _, dir := range recorder.dirs {
		if dir == "node_modules" || dir == "build" || strings.HasPrefix(dir, "node_modules/") || strings.HasPrefix(dir, "build/") {
			t.Fatalf("discovery read ignored directory %q (reads: %d)", dir, len(recorder.dirs))
		}
	}
}

func TestVaultDefinition_WatchRoots(t *testing.T) {
	root := t.TempDir()

	t.Run("classic vault returns Path", func(t *testing.T) {
		def := VaultDefinition{Path: "/some/path"}
		roots := def.WatchRoots()
		if len(roots) != 1 || roots[0] != "/some/path" {
			t.Errorf("expected [/some/path], got %v", roots)
		}
	})

	t.Run("collection with specific prefixes still watches root for system context", func(t *testing.T) {
		def := VaultDefinition{
			Root:     root,
			Includes: []string{"docs/**/*.md", "src/components/**/*.md"},
		}
		roots := def.WatchRoots()
		if len(roots) != 1 || roots[0] != root {
			t.Errorf("expected system context to require root watch [%s], got %v", root, roots)
		}
	})

	t.Run("collection with ** returns root", func(t *testing.T) {
		def := VaultDefinition{
			Root:     root,
			Includes: []string{"**/*.md"},
		}
		roots := def.WatchRoots()
		if len(roots) != 1 || roots[0] != root {
			t.Errorf("expected [%s], got %v", root, roots)
		}
	})

	t.Run("nested includes still watch root for system context", func(t *testing.T) {
		def := VaultDefinition{
			Root:     root,
			Includes: []string{"docs/**/*.md", "docs/sub/**/*.md"},
		}
		roots := def.WatchRoots()
		if len(roots) != 1 || roots[0] != root {
			t.Errorf("expected [%s], got %v", root, roots)
		}
	})
}

func TestDiscoverFiles_SkipsUnreadableSubtree(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 000 does not restrict this user")
	}
	for _, collection := range []bool{false, true} {
		root := t.TempDir()
		mkfile(t, filepath.Join(root, "A.md"))
		mkfile(t, filepath.Join(root, "locked", "B.md"))
		if err := os.Chmod(filepath.Join(root, "locked"), 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "locked"), 0o755) })

		cfg := VaultDefinition{Path: root}
		if collection {
			cfg = VaultDefinition{Root: root, Includes: []string{"**/*.md"}}
		}
		files, err := DiscoverFiles(cfg)
		if err != nil {
			t.Fatalf("collection=%v: unexpected error: %v", collection, err)
		}
		if !slices.Equal(files, []string{"A.md"}) {
			t.Fatalf("collection=%v: expected [A.md], got %v", collection, files)
		}
	}
}

func TestDiscoverFiles_UnreadableRootFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 000 does not restrict this user")
	}
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "A.md"))
	if err := os.Chmod(root, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	if _, err := DiscoverFiles(VaultDefinition{Path: root}); err == nil {
		t.Fatal("expected error for unreadable root")
	}
}

func TestDiscoverFiles_MalformedIncludeFails(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "A.md"))

	_, err := DiscoverFiles(VaultDefinition{Root: root, Includes: []string{"[bad.md"}})
	if !errors.Is(err, doublestar.ErrBadPattern) {
		t.Fatalf("expected ErrBadPattern, got %v", err)
	}
}

func TestDiscoverFiles_SymlinkContainment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	outside := t.TempDir()
	mkfile(t, filepath.Join(outside, "secret.md"))
	for _, collection := range []bool{false, true} {
		root := t.TempDir()
		mkfile(t, filepath.Join(root, "real.md"))
		if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "escape.md")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "real.md"), filepath.Join(root, "alias.md")); err != nil {
			t.Fatal(err)
		}

		cfg := VaultDefinition{Path: root}
		if collection {
			cfg = VaultDefinition{Root: root, Includes: []string{"**/*.md"}}
		}
		files, err := DiscoverFiles(cfg)
		if err != nil {
			t.Fatalf("collection=%v: unexpected error: %v", collection, err)
		}
		slices.Sort(files)
		if want := []string{"alias.md", "real.md"}; !slices.Equal(files, want) {
			t.Fatalf("collection=%v: expected %v, got %v", collection, want, files)
		}
	}
}
