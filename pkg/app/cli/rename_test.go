package actions_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/stretchr/testify/assert"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := exec.Command("git", "-C", dir, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
}

func commitGitFixture(t *testing.T, dir string) {
	t.Helper()
	if err := exec.Command(
		"git", "-C", dir,
		"-c", "user.email=ci@example.com",
		"-c", "user.name=CI Tester",
		"commit", "-m", "seed",
	).Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}
}

func TestRenameNote_GitRenameWithBacklinks(t *testing.T) {
	vaultDir := t.TempDir()
	oldName := "Old Note.md"

	// Seed files
	if err := os.WriteFile(filepath.Join(vaultDir, oldName), []byte("# Old\n"), 0o644); err != nil {
		t.Fatalf("write old note: %v", err)
	}
	refContent := "Links [[Old Note]] [[Old Note|Alias]] [[Old Note#Heading]] [[Old Note#^block|Alias]] [md](Old Note.md#section) ![emb](Old Note.md)"
	if err := os.WriteFile(filepath.Join(vaultDir, "Ref.md"), []byte(refContent), 0o644); err != nil {
		t.Fatalf("write ref note: %v", err)
	}

	// Init git and track files for history-preserving rename.
	initGitRepo(t, vaultDir)
	if err := exec.Command("git", "-C", vaultDir, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	commitGitFixture(t, vaultDir)

	params := actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, vaultDir),
		Source:          "Old Note",
		Target:          "New Note",
		Overwrite:       false,
		UpdateBacklinks: true,
	}
	res, err := actions.RenameNote(namespaceVault{path: vaultDir}, params)
	assert.NoError(t, err)
	assert.Equal(t, "New Note.md", res.RenamedPath)
	assert.True(t, res.GitHistoryPreserved)
	assert.GreaterOrEqual(t, res.LinkUpdates, 5)

	updated, readErr := os.ReadFile(filepath.Join(vaultDir, "Ref.md"))
	assert.NoError(t, readErr)
	assert.Equal(t, "Links [[New Note]] [[New Note|Alias]] [[New Note#Heading]] [[New Note#^block|Alias]] [md](New Note.md#section) ![emb](New Note.md)", string(updated))

	moved, movedErr := os.ReadFile(filepath.Join(vaultDir, "New Note.md"))
	assert.NoError(t, movedErr)
	assert.Equal(t, "# Old\n", string(moved))
	_, oldErr := os.Stat(filepath.Join(vaultDir, oldName))
	assert.ErrorIs(t, oldErr, os.ErrNotExist)

	// The index must record the rename itself, not a worktree-only move.
	staged, diffErr := exec.Command("git", "-C", vaultDir, "diff", "--cached", "--name-status", "--find-renames").Output()
	assert.NoError(t, diffErr)
	assert.Equal(t, "R100\tOld Note.md\tNew Note.md\n", string(staged))
}

func TestRenameNote_TargetExistsBlocksWithoutOverwrite(t *testing.T) {
	vaultDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vaultDir, "Old.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "Old2.md"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new: %v", err)
	}

	params := actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, vaultDir),
		Source:          "Old.md",
		Target:          "Old2.md",
		Overwrite:       false,
		UpdateBacklinks: false,
	}
	_, err := actions.RenameNote(namespaceVault{path: vaultDir}, params)
	assert.EqualError(t, err, "target note already exists: Old2.md")

	source, sourceErr := os.ReadFile(filepath.Join(vaultDir, "Old.md"))
	assert.NoError(t, sourceErr)
	assert.Equal(t, "old", string(source))
	target, targetErr := os.ReadFile(filepath.Join(vaultDir, "Old2.md"))
	assert.NoError(t, targetErr)
	assert.Equal(t, "new", string(target))
}

func TestRenameNote_OverwriteExistingTargetGit(t *testing.T) {
	vaultDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vaultDir, "Old.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "Existing.md"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	initGitRepo(t, vaultDir)
	if err := exec.Command("git", "-C", vaultDir, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	commitGitFixture(t, vaultDir)

	params := actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, vaultDir),
		Source:          "Old",
		Target:          "Existing",
		Overwrite:       true,
		UpdateBacklinks: false,
	}
	res, err := actions.RenameNote(namespaceVault{path: vaultDir}, params)
	assert.NoError(t, err)
	assert.Equal(t, "Existing.md", res.RenamedPath)

	content, readErr := os.ReadFile(filepath.Join(vaultDir, "Existing.md"))
	assert.NoError(t, readErr)
	assert.Equal(t, "old", string(content))
}

func TestRenameNote_AllowsDirtyGitSource(t *testing.T) {
	vaultDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vaultDir, "Old.md"), []byte("old"), 0o644); err != nil {
		t.Fatalf("write old: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "Other.md"), []byte("other"), 0o644); err != nil {
		t.Fatalf("write other: %v", err)
	}
	initGitRepo(t, vaultDir)
	if err := exec.Command("git", "-C", vaultDir, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	commitGitFixture(t, vaultDir)
	if err := os.WriteFile(filepath.Join(vaultDir, "Old.md"), []byte("old dirty edit"), 0o644); err != nil {
		t.Fatalf("dirty old: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "Other.md"), []byte("other dirty edit"), 0o644); err != nil {
		t.Fatalf("dirty other: %v", err)
	}

	params := actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, vaultDir),
		Source:          "Old",
		Target:          "New",
		Overwrite:       false,
		UpdateBacklinks: false,
	}
	// Uncommitted edits do not block a rename, and they travel with the note.
	res, err := actions.RenameNote(namespaceVault{path: vaultDir}, params)
	assert.NoError(t, err)
	assert.Equal(t, "New.md", res.RenamedPath)

	moved, movedErr := os.ReadFile(filepath.Join(vaultDir, "New.md"))
	assert.NoError(t, movedErr)
	assert.Equal(t, "old dirty edit", string(moved))
	_, oldErr := os.Stat(filepath.Join(vaultDir, "Old.md"))
	assert.ErrorIs(t, oldErr, os.ErrNotExist)
	other, otherErr := os.ReadFile(filepath.Join(vaultDir, "Other.md"))
	assert.NoError(t, otherErr)
	assert.Equal(t, "other dirty edit", string(other))
}

func TestRenameAttachmentUpdatesLinks(t *testing.T) {
	vaultDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vaultDir, "image.png"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write attachment: %v", err)
	}
	content := "Cover ![[image.png]] and [inline](image.png)"
	if err := os.WriteFile(filepath.Join(vaultDir, "Ref.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write ref: %v", err)
	}

	params := actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, vaultDir),
		Source:          "image.png",
		Target:          "assets/image.png",
		UpdateBacklinks: true,
	}
	res, err := actions.RenameNote(namespaceVault{path: vaultDir}, params)
	assert.NoError(t, err)
	assert.Equal(t, "assets/image.png", res.RenamedPath)

	updated, readErr := os.ReadFile(filepath.Join(vaultDir, "Ref.md"))
	assert.NoError(t, readErr)
	assert.Contains(t, string(updated), "![[assets/image.png]]")
	assert.Contains(t, string(updated), "(assets/image.png)")
	_, statErr := os.Stat(filepath.Join(vaultDir, "assets/image.png"))
	assert.NoError(t, statErr)
}

func TestRenameNote_DuplicateBasenameSkipsBareLinks(t *testing.T) {
	vaultDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vaultDir, "Folder"), 0o755); err != nil {
		t.Fatalf("make folder: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(vaultDir, "Area"), 0o755); err != nil {
		t.Fatalf("make area: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "Area", "Old Note.md"), []byte("# Old root"), 0o644); err != nil {
		t.Fatalf("write area note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vaultDir, "Folder", "Old Note.md"), []byte("# Old other"), 0o644); err != nil {
		t.Fatalf("write folder note: %v", err)
	}

	refContent := strings.Join([]string{
		"[[Old Note]]",           // ambiguous, should not rewrite
		"[md](Area/Old Note.md)", // fully-qualified, should rewrite
		"[[Folder/Old Note]]",    // points to other file, should stay
	}, "\n")
	refPath := filepath.Join(vaultDir, "Ref.md")
	if err := os.WriteFile(refPath, []byte(refContent), 0o644); err != nil {
		t.Fatalf("write ref: %v", err)
	}

	params := actions.RenameParams{
		NoteMetadata: namespaceTestMetadata(t), PostApplyRefresher: namespaceTestRefresher(t, vaultDir),
		Source:          "Area/Old Note",
		Target:          "Area/New Note",
		UpdateBacklinks: true,
	}
	_, err := actions.RenameNote(namespaceVault{path: vaultDir}, params)
	assert.NoError(t, err)

	updated, readErr := os.ReadFile(refPath)
	assert.NoError(t, readErr)
	updatedStr := string(updated)
	assert.Contains(t, updatedStr, "[md](Area/New Note.md)")
	assert.Contains(t, updatedStr, "[[Old Note]]")
	assert.Contains(t, updatedStr, "[[Folder/Old Note]]")
	assert.NotContains(t, updatedStr, "[[New Note]]")
}
