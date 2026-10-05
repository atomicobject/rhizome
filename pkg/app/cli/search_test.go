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

func TestSearchNotes(t *testing.T) {
	t.Run("Successful search notes", func(t *testing.T) {
		// Arrange
		vault := &mocks.VaultManager{}
		note := &mocks.NoteReader{}
		uri := &mocks.MockUriManager{}
		fuzzyFinder := &mocks.MockFuzzyFinder{}

		vault.On("DefaultName").Return("myVault", nil)
		vault.On("Definition").Return(obsidian.VaultDefinition{Name: "myVault", Path: "/test/vault"}, nil)
		note.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md"}, nil)
		fuzzyFinder.On("Find", []string{"note1.md", "note2.md"}, mock.MatchedBy(func(label func(int) string) bool { return label(1) == "note2.md" }), mock.Anything).Return(1, nil)
		uri.On("Construct", actions.ObsOpenUrl, map[string]string{"vault": "myVault", "file": "note2.md"}).Return("obsidian://open?vault=myVault&file=note2.md")
		uri.On("Execute", "obsidian://open?vault=myVault&file=note2.md").Return(nil)

		// Act
		err := actions.SearchNotes(vault, note, uri, fuzzyFinder)

		// Assert
		assert.NoError(t, err, "Expected no error")
		vault.AssertExpectations(t)
		note.AssertExpectations(t)
		uri.AssertExpectations(t)
		fuzzyFinder.AssertExpectations(t)
	})

	t.Run("vault.DefaultName returns an error", func(t *testing.T) {
		// Arrange
		vault := &mocks.VaultManager{}
		note := &mocks.NoteReader{}
		uri := &mocks.MockUriManager{}
		fuzzyFinder := &mocks.MockFuzzyFinder{}
		expectedErr := errors.New("Failed to get default vault name")

		vault.On("DefaultName").Return("", expectedErr)

		// Act
		err := actions.SearchNotes(vault, note, uri, fuzzyFinder)

		// Assert
		assert.Equal(t, expectedErr, err)
		vault.AssertExpectations(t)
	})

	t.Run("vault.Path returns an error", func(t *testing.T) {
		// Arrange
		vault := &mocks.VaultManager{}
		note := &mocks.NoteReader{}
		uri := &mocks.MockUriManager{}
		fuzzyFinder := &mocks.MockFuzzyFinder{}
		expectedErr := errors.New("Failed to get vault definition")

		vault.On("DefaultName").Return("myVault", nil)
		vault.On("Definition").Return(obsidian.VaultDefinition{}, expectedErr)

		// Act
		err := actions.SearchNotes(vault, note, uri, fuzzyFinder)

		// Assert
		assert.Equal(t, expectedErr, err)
		vault.AssertExpectations(t)
	})

	t.Run("note.GetNotesList returns an error", func(t *testing.T) {
		// Arrange
		vault := &mocks.VaultManager{}
		note := &mocks.NoteReader{}
		uri := &mocks.MockUriManager{}
		fuzzyFinder := &mocks.MockFuzzyFinder{}
		expectedErr := errors.New("Could not get notes list")

		vault.On("DefaultName").Return("myVault", nil)
		vault.On("Definition").Return(obsidian.VaultDefinition{Name: "myVault", Path: "/test/vault"}, nil)
		note.On("GetNotesList", mock.Anything).Return(nil, expectedErr)

		// Act
		err := actions.SearchNotes(vault, note, uri, fuzzyFinder)

		// Assert
		assert.Equal(t, expectedErr, err)
		vault.AssertExpectations(t)
		note.AssertExpectations(t)
	})

	t.Run("fuzzyFinder.Find returns an error", func(t *testing.T) {
		// Arrange
		vault := &mocks.VaultManager{}
		note := &mocks.NoteReader{}
		uri := &mocks.MockUriManager{}
		fuzzyFinder := &mocks.MockFuzzyFinder{}
		expectedErr := errors.New("Could not find note")

		vault.On("DefaultName").Return("myVault", nil)
		vault.On("Definition").Return(obsidian.VaultDefinition{Name: "myVault", Path: "/test/vault"}, nil)
		note.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md"}, nil)
		fuzzyFinder.On("Find", mock.AnythingOfType("[]string"), mock.AnythingOfType("func(int) string"), mock.Anything).Return(-1, expectedErr)

		// Act
		err := actions.SearchNotes(vault, note, uri, fuzzyFinder)

		// Assert
		assert.Equal(t, expectedErr, err)
		vault.AssertExpectations(t)
		note.AssertExpectations(t)
		fuzzyFinder.AssertExpectations(t)
	})

	t.Run("uri.Execute returns an error", func(t *testing.T) {
		// Arrange
		vault := &mocks.VaultManager{}
		note := &mocks.NoteReader{}
		uri := &mocks.MockUriManager{}
		fuzzyFinder := &mocks.MockFuzzyFinder{}
		expectedErr := errors.New("Could not execute URI")

		vault.On("DefaultName").Return("myVault", nil)
		vault.On("Definition").Return(obsidian.VaultDefinition{Name: "myVault", Path: "/test/vault"}, nil)
		note.On("GetNotesList", mock.Anything).Return([]string{"note1.md", "note2.md"}, nil)
		fuzzyFinder.On("Find", mock.AnythingOfType("[]string"), mock.AnythingOfType("func(int) string"), mock.Anything).Return(0, nil)
		uri.On("Construct", actions.ObsOpenUrl, map[string]string{"vault": "myVault", "file": "note1.md"}).Return("obsidian://open?vault=myVault&file=note1.md")
		uri.On("Execute", "obsidian://open?vault=myVault&file=note1.md").Return(expectedErr)

		// Act
		err := actions.SearchNotes(vault, note, uri, fuzzyFinder)

		// Assert
		assert.Equal(t, expectedErr, err)
		vault.AssertExpectations(t)
		note.AssertExpectations(t)
		uri.AssertExpectations(t)
		fuzzyFinder.AssertExpectations(t)
	})
}
