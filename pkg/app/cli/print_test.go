package actions_test

import (
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestPrintNote(t *testing.T) {
	vaultDef := obsidian.VaultDefinition{Name: "myVault", Path: "/test/vault"}

	t.Run("Successful print note", func(t *testing.T) {
		// Arrange
		vault := &mocks.VaultManager{}
		note := &mocks.NoteReader{}

		vault.On("Definition").Return(vaultDef, nil)
		note.On("GetContents", mock.Anything, "noteToPrint.md").Return("Note content", nil)

		// Act
		content, err := actions.PrintNote(vault, note, actions.PrintParams{
			NoteName: "noteToPrint.md",
		})

		// Assert
		assert.NoError(t, err, "Expected no error")
		assert.Equal(t, "Note content", content)
		vault.AssertExpectations(t)
		note.AssertExpectations(t)
	})

	t.Run("vault.Definition returns an error", func(t *testing.T) {
		// Arrange
		vault := &mocks.VaultManager{}
		note := &mocks.NoteReader{}
		expectedErr := errors.New("Failed to get vault definition")

		vault.On("Definition").Return(obsidian.VaultDefinition{}, expectedErr)

		// Act
		content, err := actions.PrintNote(vault, note, actions.PrintParams{
			NoteName: "noteToPrint.md",
		})

		// Assert
		assert.Equal(t, expectedErr, err)
		assert.Empty(t, content)
		vault.AssertExpectations(t)
	})

	t.Run("note.GetContents returns an error", func(t *testing.T) {
		// Arrange
		vault := &mocks.VaultManager{}
		note := &mocks.NoteReader{}
		expectedErr := errors.New("Could not get contents")

		vault.On("Definition").Return(vaultDef, nil)
		note.On("GetContents", mock.Anything, "noteToPrint.md").Return("", expectedErr)

		// Act
		content, err := actions.PrintNote(vault, note, actions.PrintParams{
			NoteName: "noteToPrint.md",
		})

		// Assert
		assert.Equal(t, expectedErr, err)
		assert.Empty(t, content)
		vault.AssertExpectations(t)
		note.AssertExpectations(t)
	})
}
