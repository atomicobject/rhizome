package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected RelPath
	}{
		{"empty", "", RelPath("")},
		{"simple", "foo/bar", RelPath("foo/bar")},
		{"with backslash", "foo\\bar", RelPath("foo/bar")},
		{"with dot prefix", "./foo/bar", RelPath("foo/bar")},
		{"with dot dot prefix", "../foo/bar", RelPath("foo/bar")},
		{"nested dot prefix", "././foo/bar", RelPath("./foo/bar")}, // Only removes single level
		{"windows style", "C:\\Users\\test", RelPath("C:/Users/test")},
		{"mixed separators", "foo\\bar/baz", RelPath("foo/bar/baz")},
		{"leading slash", "/foo/bar", RelPath("/foo/bar")},
		{"trailing slash", "foo/bar/", RelPath("foo/bar/")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Normalize(tt.input)
			if result != tt.expected {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeNote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected NotePath
	}{
		{"empty", "", NotePath(".md")},
		{"no suffix", "foo/bar", NotePath("foo/bar.md")},
		{"has suffix", "foo/bar.md", NotePath("foo/bar.md")},
		{"has uppercase suffix", "foo/Decision.MD", NotePath("foo/Decision.MD")},
		{"has mixed-case suffix", "foo/Decision.mD", NotePath("foo/Decision.mD")},
		{"with dot prefix", "./foo/bar", NotePath("foo/bar.md")},
		{"windows style", "foo\\bar", NotePath("foo/bar.md")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeNote(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeNote(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeDocPathPreservesExplicitNoteIdentity(t *testing.T) {
	vaultPaths, err := NewVaultPaths(t.TempDir())
	if err != nil {
		t.Fatalf("NewVaultPaths: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"uppercase markdown", "notes/Decision.MD", "notes/Decision.MD"},
		{"mixed-case markdown", "notes/Decision.mD", "notes/Decision.mD"},
		{"html", "notes/Decision.HTML", "notes/Decision.HTML"},
		{"extensionless", "notes/Decision", "notes/Decision"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vaultPaths.NormalizeDocPath(tt.input); got != tt.expected {
				t.Errorf("NormalizeDocPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestNormalizeNotePathDoesNotInferMarkdownSuffix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected NotePath
	}{
		{"empty", "", NotePath("")},
		{"extensionless", "notes/decision", NotePath("notes/decision")},
		{"html", "notes/decision.html", NotePath("notes/decision.html")},
		{"preserves extension casing", "notes/Decision.HTML", NotePath("notes/Decision.HTML")},
		{"normalizes separators", "./notes\\decision.htm", NotePath("notes/decision.htm")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeNotePath(tt.input); got != tt.expected {
				t.Errorf("NormalizeNotePath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCleanNotePathRejectsNonVaultRelativeIdentity(t *testing.T) {
	for _, input := range []string{"", "/outside/note.html", "../outside/note.html", "C:\\outside\\note.html"} {
		if _, err := CleanNotePath(input); err == nil {
			t.Fatalf("CleanNotePath(%q) succeeded", input)
		}
	}

	path, err := CleanNotePath("./notes/decision.HTML")
	if err != nil {
		t.Fatalf("CleanNotePath valid path: %v", err)
	}
	if path != "notes/decision.HTML" {
		t.Fatalf("CleanNotePath = %q, want notes/decision.HTML", path)
	}
}

func TestNormalizeCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected CodePath
	}{
		{"empty", "", CodePath("")},
		{"simple", "foo/bar.go", CodePath("foo/bar.go")},
		{"with dot prefix", "./foo/bar.go", CodePath("foo/bar.go")},
		{"windows style", "foo\\bar.go", CodePath("foo/bar.go")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeCode(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeCode(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToAbs(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "foo", "bar"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		rel  RelPath
		want AbsPath
	}{
		{"empty", "", ""},
		{"simple relative", "foo/bar", AbsPath(filepath.Join(root, "foo", "bar"))},
		{"with dot prefix", "./foo/bar", AbsPath(filepath.Join(root, "foo", "bar"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			abs, err := ToAbs(tt.rel, tmpDir)
			if err != nil || abs != tt.want {
				t.Errorf("ToAbs(%q, %q) = %q, %v; want %q", tt.rel, tmpDir, abs, err, tt.want)
			}
		})
	}
}

func TestToAbsWindowsPOSIX(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	// On Windows, POSIX-style paths starting with / should be preserved
	rel := RelPath("/repo/foo/bar")
	abs, err := ToAbs(rel, "C:\\test")
	if err != nil {
		t.Fatalf("ToAbs error: %v", err)
	}

	// Should preserve the POSIX-style path, not convert to Windows drive path
	if !strings.HasPrefix(string(abs), "/") {
		t.Errorf("ToAbs preserved POSIX path %q, got %q", rel, abs)
	}
}

func TestToRel(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "foo", "bar.txt")
	if err := os.MkdirAll(filepath.Dir(testFile), 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	root, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	abs := AbsPath(filepath.Join(root, "foo", "bar.txt"))

	rel, err := ToRel(abs, tmpDir)
	if err != nil {
		t.Fatalf("ToRel error: %v", err)
	}

	expected := RelPath("foo/bar.txt")
	if rel != expected {
		t.Errorf("ToRel(%q, %q) = %q, want %q", abs, tmpDir, rel, expected)
	}
}

func TestResolveSymlinks(t *testing.T) {
	tmpDir := t.TempDir()

	realFile := filepath.Join(tmpDir, "real.txt")
	if err := os.WriteFile(realFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create real file: %v", err)
	}

	link := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink(realFile, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(realFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveSymlinks(link); got != AbsPath(filepath.ToSlash(resolved)) {
		t.Errorf("ResolveSymlinks(%q) = %q, want %q", link, got, resolved)
	}

	missing := filepath.Join(tmpDir, "missing.txt")
	if got := ResolveSymlinks(missing); got != AbsPath(filepath.ToSlash(missing)) {
		t.Errorf("ResolveSymlinks(%q) = %q, want %q", missing, got, missing)
	}
}

func TestResolveSymlinksWindowsPOSIX(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	// POSIX-style path should be preserved on Windows
	posixPath := "/repo/foo/bar"
	abs := ResolveSymlinks(posixPath)

	if !strings.HasPrefix(string(abs), "/") {
		t.Errorf("ResolveSymlinks preserved POSIX path %q, got %q", posixPath, abs)
	}
}
