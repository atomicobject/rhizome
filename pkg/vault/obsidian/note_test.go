package obsidian_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
)

func TestNote_GetContents(t *testing.T) {
	tests := []struct {
		testName           string
		noteToCreate       string
		noteNameToRetrieve string
	}{
		{"Get contents of note", "note.md", "note.md"},
		{"Get contents of note without md", "note.md", "note"},
	}
	for _, test := range tests {
		t.Run(test.testName, func(t *testing.T) {
			// Arrange
			tempDir := t.TempDir()
			vaultPath := "vault-folder"
			notePath := filepath.Join(tempDir, vaultPath, test.noteToCreate)
			fileContents := "Example file contents here"

			err := os.MkdirAll(filepath.Join(tempDir, vaultPath), 0755)
			if err != nil {
				t.Fatal(err)
			}

			err = os.WriteFile(notePath, []byte(fileContents), 0644)
			if err != nil {
				t.Fatal(err)
			}

			// Act
			noteManager := obsidian.Note{}
			vaultDef := obsidian.VaultDefinition{Name: "test", Path: filepath.Join(tempDir, vaultPath)}
			content, err := noteManager.GetContents(vaultDef, test.noteNameToRetrieve)

			// Assert
			assert.Equal(t, nil, err, "Expected no error while retrieving note contents")
			assert.Equal(t, fileContents, content, "Expected contents to match the file contents")
		})
	}

	t.Run("Get contents of non-existent note", func(t *testing.T) {
		// Arrange
		noteManager := obsidian.Note{}
		vaultDef := obsidian.VaultDefinition{Name: "test", Path: "path"}
		// Act
		contents, err := noteManager.GetContents(vaultDef, "non-existent-note")
		// Assert
		assert.Equal(t, obsidian.NoteDoesNotExistError, err.Error(), "Expected error while deleting non-existent note")
		assert.Equal(t, contents, "")

	})
}

func TestNote_ReadsDiscoveredAuthoredExtensionCasing(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "Decision.MD")
	if err := os.WriteFile(path, []byte("preserve casing"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	wantModTime := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, wantModTime, wantModTime); err != nil {
		t.Fatalf("set note mod time: %v", err)
	}

	vault := obsidian.VaultDefinition{Name: "test", Path: tempDir}
	note := obsidian.Note{}
	discovered, err := note.GetNotesList(vault)
	if err != nil {
		t.Fatalf("GetNotesList: %v", err)
	}
	if len(discovered) != 1 || discovered[0] != "Decision.MD" {
		t.Fatalf("GetNotesList = %v, want [Decision.MD]", discovered)
	}

	content, err := note.GetContents(vault, discovered[0])
	if err != nil {
		t.Fatalf("GetContents: %v", err)
	}
	if content != "preserve casing" {
		t.Fatalf("GetContents = %q, want preserved contents", content)
	}
	gotModTime, err := note.GetModTime(vault, discovered[0])
	if err != nil {
		t.Fatalf("GetModTime: %v", err)
	}
	if !gotModTime.Equal(wantModTime) {
		t.Fatalf("GetModTime = %v, want %v", gotModTime, wantModTime)
	}
}

func TestNote_ExtensionlessInputStillUsesMarkdownFallback(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "legacy.md")
	if err := os.WriteFile(path, []byte("legacy"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	vault := obsidian.VaultDefinition{Name: "test", Path: tempDir}
	note := obsidian.Note{}
	content, err := note.GetContents(vault, "legacy")
	if err != nil {
		t.Fatalf("GetContents: %v", err)
	}
	if content != "legacy" {
		t.Fatalf("GetContents = %q, want legacy contents", content)
	}
	if _, err := note.GetModTime(vault, "legacy"); err != nil {
		t.Fatalf("GetModTime: %v", err)
	}
}

func TestNote_NonMarkdownInputKeepsLegacyMarkdownFallback(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "legacy.txt"), []byte("not a note"), 0o644); err != nil {
		t.Fatalf("write non-note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "legacy.txt.md"), []byte("legacy note"), 0o644); err != nil {
		t.Fatalf("write compatibility note: %v", err)
	}

	vault := obsidian.VaultDefinition{Name: "test", Path: tempDir}
	note := obsidian.Note{}
	content, err := note.GetContents(vault, "legacy.txt")
	if err != nil {
		t.Fatalf("GetContents: %v", err)
	}
	if content != "legacy note" {
		t.Fatalf("GetContents = %q, want legacy Markdown fallback", content)
	}
	if _, err := note.GetModTime(vault, "legacy.txt"); err != nil {
		t.Fatalf("GetModTime: %v", err)
	}
}

func TestNote_ReadsExplicitlyAdmittedNonMarkdownNote(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "report.html"), []byte("<h1>Report</h1>"), 0o644); err != nil {
		t.Fatalf("write HTML note: %v", err)
	}

	note := obsidian.Note{}
	admitted := obsidian.VaultDefinition{Root: tempDir, Includes: []string{"**/*.html"}}
	content, err := note.GetContents(admitted, "report.html")
	if err != nil {
		t.Fatalf("GetContents admitted HTML: %v", err)
	}
	if content != "<h1>Report</h1>" {
		t.Fatalf("GetContents = %q", content)
	}
	if _, err := note.GetModTime(admitted, "report.html"); err != nil {
		t.Fatalf("GetModTime admitted HTML: %v", err)
	}

	notAdmitted := obsidian.VaultDefinition{Root: tempDir, Includes: []string{"**/*.md"}}
	if _, err := note.GetContents(notAdmitted, "report.html"); err == nil {
		t.Fatal("GetContents unexpectedly read HTML outside configured note admission")
	}
}

func createTmpDirAndFiles(t *testing.T, perm os.FileMode, files []string, content []byte) string {
	t.Helper()
	// Create a temporary test directory
	tmpDir := t.TempDir()
	for _, file := range files {
		err := os.WriteFile(filepath.Join(tmpDir, file), content, perm)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}
	// create other non markdown files
	err := os.WriteFile(filepath.Join(tmpDir, "file4.txt"), []byte("This is a test file"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	// create hidden directory
	err = os.Mkdir(filepath.Join(tmpDir, ".hidden"), 0644)
	if err != nil {
		t.Fatalf("Failed to create hidden directory: %v", err)
	}
	return tmpDir
}

func TestNote_GetNotesList(t *testing.T) {
	t.Run("Retrieve list of notes successfully", func(t *testing.T) {
		// Arrange
		testFiles := []string{"file1.md", "file2.md", "file3.md"}
		content := []byte("This is a test note")
		tmpDir := createTmpDirAndFiles(t, 0644, testFiles, content)
		vaultDef := obsidian.VaultDefinition{Name: "test", Path: tmpDir}

		noteManager := obsidian.Note{}

		// Act
		notes, err := noteManager.GetNotesList(vaultDef)

		// Assert
		assert.NoError(t, err, "Expected no error while retrieving notes list")
		assert.ElementsMatch(t, testFiles, notes, "Expected notes list to match the created files")
	})

	t.Run("Empty vault directory", func(t *testing.T) {
		// Arrange
		tmpDir := t.TempDir()
		vaultDef := obsidian.VaultDefinition{Name: "test", Path: tmpDir}
		noteManager := obsidian.Note{}

		// Act
		notes, err := noteManager.GetNotesList(vaultDef)

		// Assert
		assert.NoError(t, err, "Expected no error for empty vault directory")
		assert.Empty(t, notes, "Expected empty notes list for empty vault directory")
	})

	t.Run("Vault directory with non-Markdown files", func(t *testing.T) {
		// Arrange
		tmpDir := createTmpDirAndFiles(t, 0644, []string{"file1.txt", "file2.jpg"}, []byte("Non-markdown content"))
		vaultDef := obsidian.VaultDefinition{Name: "test", Path: tmpDir}
		noteManager := obsidian.Note{}

		// Act
		notes, err := noteManager.GetNotesList(vaultDef)

		// Assert
		assert.NoError(t, err, "Expected no error when non-Markdown files are present")
		assert.Empty(t, notes, "Expected empty notes list when no Markdown files are present")
	})
}
