package actions_test

import (
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/stretchr/testify/assert"
)

func TestCreateNote(t *testing.T) {
	const constructed = "obsidian://new?constructed"

	tests := []struct {
		name   string
		params actions.CreateParams
		want   map[string]string
	}{
		{
			name:   "defaults",
			params: actions.CreateParams{NoteName: "note.md"},
			want:   map[string]string{"vault": "myVault", "append": "false", "overwrite": "false", "content": "", "file": "note.md", "silent": "true"},
		},
		{
			name:   "append and open",
			params: actions.CreateParams{NoteName: "note.md", ShouldAppend: true, ShouldOpen: true, Content: "Plain text with no escapes"},
			want:   map[string]string{"vault": "myVault", "append": "true", "overwrite": "false", "content": "Plain text with no escapes", "file": "note.md", "silent": "false"},
		},
		{
			name:   "overwrite normalizes escape sequences",
			params: actions.CreateParams{NoteName: "dir/note.md", ShouldOverwrite: true, Content: "Hello\\nWorld\\tTabbed\\rReturn\\\"Quote\\'SingleQuote\\\\Backslash"},
			want:   map[string]string{"vault": "myVault", "append": "false", "overwrite": "true", "content": "Hello\nWorld\tTabbed\rReturn\"Quote'SingleQuote\\Backslash", "file": "dir/note.md", "silent": "true"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vault := &mocks.VaultManager{}
			uri := &mocks.MockUriManager{}
			vault.On("DefaultName").Return("myVault", nil)
			uri.On("Construct", "obsidian://new", tt.want).Return(constructed).Once()
			uri.On("Execute", constructed).Return(nil).Once()

			assert.NoError(t, actions.CreateNote(vault, uri, tt.params))
			vault.AssertExpectations(t)
			uri.AssertExpectations(t)
		})
	}

	t.Run("vault.DefaultName returns an error", func(t *testing.T) {
		vault := &mocks.VaultManager{}
		uri := &mocks.MockUriManager{}
		expectedErr := errors.New("Failed to get vault name")
		vault.On("DefaultName").Return("", expectedErr)

		err := actions.CreateNote(vault, uri, actions.CreateParams{NoteName: "note.md"})

		assert.Equal(t, expectedErr, err)
		vault.AssertExpectations(t)
		uri.AssertNotCalled(t, "Execute", constructed)
	})

	t.Run("uri.Execute returns an error", func(t *testing.T) {
		vault := &mocks.VaultManager{}
		uri := &mocks.MockUriManager{}
		expectedErr := errors.New("Failed to execute URI")
		vault.On("DefaultName").Return("myVault", nil)
		uri.On("Construct", "obsidian://new", map[string]string{"vault": "myVault", "append": "false", "overwrite": "false", "content": "", "file": "note.md", "silent": "true"}).Return(constructed)
		uri.On("Execute", constructed).Return(expectedErr)

		err := actions.CreateNote(vault, uri, actions.CreateParams{NoteName: "note.md"})

		assert.Equal(t, expectedErr, err)
		vault.AssertExpectations(t)
		uri.AssertExpectations(t)
	})
}
