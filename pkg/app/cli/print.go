package actions

import (
	"errors"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type PrintParams struct {
	NoteName string
}

func PrintNote(vault obsidian.VaultManager, note obsidian.NoteReader, params PrintParams) (string, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		return "", err
	}
	vaultPath := vaultDef.BasePath()
	if vaultPath == "" {
		return "", errors.New(obsidian.RhizomeVaultPathInvalidError)
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return "", errors.New(obsidian.RhizomeVaultPathInvalidError)
	}
	rel, _, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, params.NoteName)
	if err != nil || rel == "" {
		return "", errors.New(obsidian.NoteDoesNotExistError)
	}

	contents, err := note.GetContents(vaultDef, rel.String())
	if err != nil {
		return "", err
	}

	return contents, nil
}
