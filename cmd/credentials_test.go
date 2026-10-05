package cmd

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialsImportPassesMappingsWithoutWritingValues(t *testing.T) {
	var gotAccount string
	var gotKeys []string
	command := newCredentialsCommand(func(_ context.Context, account string, keys []string) (int, error) {
		gotAccount = account
		gotKeys = append([]string(nil), keys...)
		return 2, nil
	})
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"import", "--from", "1password", "--account", "example.1password.com", "--key", "VOYAGE_API_KEY=op://Private/voyage/credential", "--key", "TYPESAFE_API_KEY=op://Private/typesafe/credential"})
	require.NoError(t, command.Execute())
	require.Equal(t, "example.1password.com", gotAccount)
	require.Equal(t, []string{"VOYAGE_API_KEY=op://Private/voyage/credential", "TYPESAFE_API_KEY=op://Private/typesafe/credential"}, gotKeys)
	require.Equal(t, "Imported 2 credential(s) into ~/.config/rhizome/config.yml\n", output.String())
}

func TestCredentialsImportHelpDocumentsStableReferences(t *testing.T) {
	command := newCredentialsCommand(func(context.Context, string, []string) (int, error) { return 0, nil })
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"import", "--help"})
	require.NoError(t, command.Execute())
	require.Contains(t, output.String(), "KEY=op://vault/item/field")
	require.Contains(t, output.String(), "--from")
}
