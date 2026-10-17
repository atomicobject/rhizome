package operations

import (
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

type OpenParams struct {
	NoteName string
}

func OpenNote(vault repoconfig.VaultManager, uri obsidian.UriManager, params OpenParams) error {
	vaultName, err := vault.DefaultName()
	if err != nil {
		return err
	}

	obsidianUri := uri.Construct(ObsOpenUrl, map[string]string{
		"vault": vaultName,
		"file":  params.NoteName,
	})

	err = uri.Execute(obsidianUri)
	if err != nil {
		return err
	}
	return nil
}
