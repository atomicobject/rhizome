package operations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestDirectMutationsRefuseOwnedHostsBeforeAnyFileChanges(t *testing.T) {
	for _, action := range []string{"property", "tags", "delete", "append", "overwrite", "create"} {
		t.Run(action, func(t *testing.T) {
			root := t.TempDir()
			original := "---\nstatus: old\n---\n# Existing\n"
			for _, name := range []string{"mine.md", "owned.md"} {
				require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(original), 0644))
			}
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".rhizome"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome/source-ownership.yml"), []byte("sources:\n  - owner: external\n    reason: Use external authoring.\n    paths: [owned.md, new.md]\n"), 0644))
			vault := stubVault{path: root}
			uri := &mocks.MockUriManager{}
			var err error
			switch action {
			case "property":
				_, err = SetPropertyOnFilesWithWorkers(t.Context(), vault, &obsidian.Note{}, "status", "new", []string{"mine.md", "owned.md"}, true, false, 2)
			case "tags":
				_, err = AddTagsToFilesWithWorkers(t.Context(), vault, &obsidian.Note{}, []string{"new"}, []string{"mine.md", "owned.md"}, false, 2)
			case "delete":
				err = DeleteNote(vault, DeleteParams{Context: t.Context(), NotePath: "owned.md"})
			case "append":
				err = CreateNote(vault, uri, CreateParams{NoteName: "owned.md", Content: "new", ShouldAppend: true})
			case "overwrite":
				err = CreateNote(vault, uri, CreateParams{NoteName: "./owned.md", Content: "new", ShouldOverwrite: true})
			case "create":
				err = CreateNote(vault, uri, CreateParams{NoteName: "new.md", Content: "new"})
			}
			require.ErrorContains(t, err, "Use external authoring.")
			uri.AssertNotCalled(t, "Construct", ObsCreateUrl, mock.Anything)
			uri.AssertNotCalled(t, "Execute", mock.Anything)
			for _, name := range []string{"mine.md", "owned.md"} {
				data, err := os.ReadFile(filepath.Join(root, name))
				require.NoError(t, err)
				require.Equal(t, original, string(data))
			}
			require.NoFileExists(t, filepath.Join(root, "new.md"))
		})
	}
}
