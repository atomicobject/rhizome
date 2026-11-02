package procrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLookLoginPathFindsWindowsExecutableExtensions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fake-tool.bat"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := LookLoginPath("fake-tool")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(got, filepath.Join(dir, "fake-tool.bat")) {
		t.Fatalf("LookLoginPath = %q", got)
	}
}
