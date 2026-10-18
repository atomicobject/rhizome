package operations

import (
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func DailyNote(vault repoconfig.VaultManager, uri obsidian.UriManager) error {
	vaultName, err := vault.DefaultName()
	if err != nil {
		return err
	}

	obsidianUri := uri.Construct(OnsDailyUrl, map[string]string{
		"vault": vaultName,
	})

	err = uri.Execute(obsidianUri)
	if err != nil {
		return err
	}
	return nil
}
