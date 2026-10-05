package actions_test

import (
	"errors"
	"testing"

	"github.com/atomicobject/rhizome/mocks"
	"github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/stretchr/testify/assert"
)

func TestDailyNote(t *testing.T) {
	const constructed = "obsidian://daily?constructed"

	t.Run("Successful creates / opens daily note", func(t *testing.T) {
		vault := &mocks.VaultManager{}
		uri := &mocks.MockUriManager{}
		vault.On("DefaultName").Return("myVault", nil)
		uri.On("Construct", "obsidian://daily", map[string]string{"vault": "myVault"}).Return(constructed).Once()
		uri.On("Execute", constructed).Return(nil).Once()

		assert.NoError(t, actions.DailyNote(vault, uri))
		vault.AssertExpectations(t)
		uri.AssertExpectations(t)
	})

	t.Run("vault.DefaultName returns an error", func(t *testing.T) {
		vault := &mocks.VaultManager{}
		uri := &mocks.MockUriManager{}
		expectedErr := errors.New("Failed to get vault name")
		vault.On("DefaultName").Return("", expectedErr)

		err := actions.DailyNote(vault, uri)

		assert.Equal(t, expectedErr, err)
		vault.AssertExpectations(t)
		uri.AssertNotCalled(t, "Execute", constructed)
	})

	t.Run("uri.Execute returns an error", func(t *testing.T) {
		vault := &mocks.VaultManager{}
		uri := &mocks.MockUriManager{}
		expectedErr := errors.New("Failed to execute URI")
		vault.On("DefaultName").Return("myVault", nil)
		uri.On("Construct", "obsidian://daily", map[string]string{"vault": "myVault"}).Return(constructed)
		uri.On("Execute", constructed).Return(expectedErr)

		err := actions.DailyNote(vault, uri)

		assert.Equal(t, expectedErr, err)
		vault.AssertExpectations(t)
		uri.AssertExpectations(t)
	})
}
