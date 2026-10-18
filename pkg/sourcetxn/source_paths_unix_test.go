//go:build !windows

package sourcetxn

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceRefusesSymlinkedParentsBeforeAnyPublication(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "single aliased path", true: "two authored paths one target"}[duplicate], func(t *testing.T) {
			root, options, _ := sourceFixture(t)
			require.NoError(t, os.Mkdir(filepath.Join(root, "real"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "real", "a.md"), []byte("old"), 0640))
			require.NoError(t, os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")))
			intent := Intent{Fingerprint: Hash([]byte("aliased")), Files: []FilePatch{{Path: "alias/a.md", Original: []byte("old"), OriginalMode: 0640, Updated: []byte("new")}}}
			if duplicate {
				intent.Files = append(intent.Files, FilePatch{Path: "real/a.md", Original: []byte("old"), OriginalMode: 0640, Updated: []byte("second")})
			}
			require.Error(t, applySourceFixture(context.Background(), root, intent, options))
			content, err := os.ReadFile(filepath.Join(root, "real", "a.md"))
			require.NoError(t, err)
			require.Equal(t, "old", string(content))
			require.NoError(t, CheckPending(root), "unsafe paths must fail before journal or artifact creation")
		})
	}
}

func TestSourceRefusesSymlinkAndHardlinkSources(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root, options, intent := sourceFixture(t)
			alias := filepath.Join(root, "alias.md")
			if kind == "symlink" {
				require.NoError(t, os.Symlink(filepath.Join(root, "a.md"), alias))
			} else {
				require.NoError(t, os.Link(filepath.Join(root, "a.md"), alias))
			}
			intent.Files = []FilePatch{{Path: "alias.md", Original: []byte("old"), OriginalMode: 0640, Updated: []byte("new")}}
			err := applySourceFixture(context.Background(), root, intent, options)
			require.ErrorIs(t, err, ErrSourceChanged)
			require.Equal(t, "alias.md", ConflictPath(err))
			content, err := os.ReadFile(filepath.Join(root, "a.md"))
			require.NoError(t, err)
			require.Equal(t, "old", string(content))
			require.NoError(t, CheckPending(root))
		})
	}
}

func TestSourceRechecksParentAfterPublicationHook(t *testing.T) {
	root, options, _ := sourceFixture(t)
	for _, dir := range []string{"real", "other"} {
		require.NoError(t, os.Mkdir(filepath.Join(root, dir), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(root, dir, "a.md"), []byte("old"), 0640))
	}
	intent := Intent{Fingerprint: Hash([]byte("parent-swap")), Files: []FilePatch{{Path: "real/a.md", Original: []byte("old"), OriginalMode: 0640, Updated: []byte("new")}}}
	err := apply(context.Background(), root, intent, options, &Hooks{BeforeInstall: func(int, string) error {
		require.NoError(t, os.Rename(filepath.Join(root, "real"), filepath.Join(root, "moved")))
		return os.Symlink(filepath.Join(root, "other"), filepath.Join(root, "real"))
	}})
	require.ErrorIs(t, err, ErrSourceChanged)
	require.Equal(t, "real/a.md", ConflictPath(err))
	for _, dir := range []string{"moved", "other"} {
		content, err := os.ReadFile(filepath.Join(root, dir, "a.md"))
		require.NoError(t, err)
		require.Equal(t, "old", string(content))
	}
}
