package modulelifecycle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func linkInput(t *testing.T, root, target string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestFingerprintTracksDeclaredLinkedFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "AGENTS.md")
	require.NoError(t, os.WriteFile(file, []byte("first"), 0o600))
	linkInput(t, root, "AGENTS.md")
	d := Descriptor{Module: "demo", Inputs: []string{"CLAUDE.md"}}
	first, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, []byte("changed"), 0o600))
	changed, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NotEqual(t, first, changed)
	require.NoError(t, os.WriteFile(filepath.Join(root, "OTHER.md"), []byte("changed"), 0o600))
	require.NoError(t, os.Remove(filepath.Join(root, "CLAUDE.md")))
	linkInput(t, root, "OTHER.md")
	retargeted, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NotEqual(t, changed, retargeted)
}

func TestFingerprintRejectsUnsafeLinkedInputs(t *testing.T) {
	for _, kind := range []string{"outside", "absolute-inside", "absolute-outside", "missing", "cycle", "directory", "directory-link", "linked-parent", "missing-parent", "file-parent", "trailing-slash", "git", "discovered"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("inside"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(outside, "AGENTS.md"), []byte("outside"), 0o600))
			d := Descriptor{Module: "demo", Inputs: []string{"CLAUDE.md"}}
			target := "AGENTS.md"
			switch kind {
			case "outside":
				var err error
				target, err = filepath.Rel(root, filepath.Join(outside, "AGENTS.md"))
				require.NoError(t, err)
			case "absolute-inside":
				target = filepath.Join(root, "AGENTS.md")
			case "absolute-outside":
				target = filepath.Join(outside, "AGENTS.md")
			case "missing":
				target = "missing.md"
			case "cycle":
				target = "CLAUDE.md"
			case "directory":
				target = "documents"
				require.NoError(t, os.Mkdir(filepath.Join(root, target), 0o700))
			case "directory-link":
				require.NoError(t, os.Mkdir(filepath.Join(root, "documents"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(root, "documents", "AGENTS.md"), []byte("inside"), 0o600))
				if err := os.Symlink("documents", filepath.Join(root, "alias")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				target = "alias/AGENTS.md"
			case "linked-parent":
				require.NoError(t, os.MkdirAll(filepath.Join(root, "nested", "dir"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "AGENTS.md"), []byte("different"), 0o600))
				if err := os.Symlink("nested/dir", filepath.Join(root, "alias")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				target = "alias/../AGENTS.md"
			case "missing-parent":
				target = "missing/../AGENTS.md"
			case "file-parent":
				target = "AGENTS.md/../AGENTS.md"
			case "trailing-slash":
				target = "AGENTS.md/"
			case "git":
				require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "config"), []byte("fixture metadata"), 0o600))
				target = ".git/config"
			case "discovered":
				require.NoError(t, os.Mkdir(filepath.Join(root, "documents"), 0o700))
				if err := os.Symlink("../AGENTS.md", filepath.Join(root, "documents", "linked.md")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				d.Inputs = []string{"documents"}
			}
			linkInput(t, root, target)
			_, err := Fingerprint(t.Context(), root, d)
			require.Error(t, err)
		})
	}
}

func TestLinkedInputPreservesParentTraversalSemantics(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("inside"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "docs"), 0o700))
	linkInput(t, root, "docs/../AGENTS.md")
	d := Descriptor{Module: "demo", Inputs: []string{"CLAUDE.md"}}
	_, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(root, "docs")))
	_, err = Fingerprint(t.Context(), root, d)
	require.Error(t, err)
}

func TestLinkedInputCancellation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("inside"), 0o600))
	linkInput(t, root, "AGENTS.md")
	dir, err := os.OpenRoot(root)
	require.NoError(t, err)
	defer dir.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = readLinkedInputFile(ctx, dir, "CLAUDE.md", 1024)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLinkedInputsShareTheFingerprintByteBudget(t *testing.T) {
	root := t.TempDir()
	file, err := os.Create(filepath.Join(root, "AGENTS.md"))
	require.NoError(t, err)
	require.NoError(t, file.Truncate(33<<20))
	require.NoError(t, file.Close())
	linkInput(t, root, "AGENTS.md")
	_, err = Fingerprint(t.Context(), root, Descriptor{Module: "demo", Inputs: []string{"AGENTS.md", "CLAUDE.md"}})
	require.ErrorContains(t, err, "exceeds")
}

func TestStrictReadsStillRejectAnInternalLink(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("inside"), 0o600))
	linkInput(t, root, "AGENTS.md")
	_, err := ReadRegularFile(root, "CLAUDE.md", 1024)
	require.ErrorContains(t, err, "regular file")
}
