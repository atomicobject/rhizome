package actions_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
)

func TestDeleteNote(t *testing.T) {
	t.Run("Successful delete note", func(t *testing.T) {
		tmpDir := t.TempDir()
		notePath := filepath.Join(tmpDir, "note.md")
		assert.NoError(t, os.WriteFile(notePath, []byte("content"), 0o644))
		vault := stubVault{def: obsidian.VaultDefinition{Name: "test", Path: tmpDir}}

		// Act
		err := actions.DeleteNote(vault, actions.DeleteParams{
			NotePath: "note",
		})

		// Assert
		assert.NoError(t, err, "Expected no error")
		_, statErr := os.Stat(notePath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("vault.Definition returns an error", func(t *testing.T) {
		// Arrange
		expectedErr := errors.New("Failed to get default vault name")
		vault := stubVault{err: expectedErr}

		// Act
		err := actions.DeleteNote(vault, actions.DeleteParams{
			NotePath: "noteToDelete",
		})

		// Assert
		assert.Equal(t, expectedErr, err)
	})

	t.Run("note does not exist", func(t *testing.T) {
		// Arrange
		tmpDir := t.TempDir()
		vault := stubVault{def: obsidian.VaultDefinition{Name: "test", Path: tmpDir}}

		// Act
		err := actions.DeleteNote(vault, actions.DeleteParams{
			NotePath: "noteToDelete",
		})

		// Assert
		assert.EqualError(t, err, obsidian.NoteDoesNotExistError)
	})

	t.Run("vault path invalid", func(t *testing.T) {
		// Arrange
		vault := stubVault{def: obsidian.VaultDefinition{Name: "test", Path: ""}}

		// Act
		err := actions.DeleteNote(vault, actions.DeleteParams{
			NotePath: "noteToDelete",
		})

		// Assert
		assert.EqualError(t, err, obsidian.RhizomeVaultPathInvalidError)
	})
}

type stubVault struct {
	def obsidian.VaultDefinition
	err error
}

func (s stubVault) DefaultName() (string, error) {
	return s.def.Name, s.err
}

func (s stubVault) SetDefaultName(name string) error {
	return s.err
}

func (s stubVault) Path() (string, error) {
	return s.def.Path, s.err
}

func (s stubVault) Definition() (obsidian.VaultDefinition, error) {
	if s.err != nil {
		return obsidian.VaultDefinition{}, s.err
	}
	return s.def, nil
}
