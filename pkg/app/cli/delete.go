package actions

import (
	"context"
	"errors"
	"os"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type DeleteParams struct {
	Context  context.Context
	NotePath string
}

func DeleteNote(vault obsidian.VaultManager, params DeleteParams) error {
	vaultDef, err := vault.Definition()
	if err != nil {
		return err
	}
	vaultPath := vaultDef.BasePath()
	if vaultPath == "" {
		return errors.New(obsidian.RhizomeVaultPathInvalidError)
	}
	release, err := acquireWriteLock(params.Context, vaultPath)
	if err != nil {
		return err
	}
	defer release()
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return errors.New(obsidian.RhizomeVaultPathInvalidError)
	}
	// note delete retains the Markdown CLI's extensionless input convention.
	// Format-neutral paths belong to read/index surfaces; this command's
	// established `note` input addresses `note.md`.
	_, abs, err := paths.ResolveNoteInputWithVaultPaths(vaultPaths, params.NotePath)
	if err != nil || abs == "" {
		return errors.New(obsidian.NoteDoesNotExistError)
	}
	if params.Context != nil {
		if err := params.Context.Err(); err != nil {
			return err
		}
	}
	if err := os.Remove(abs.String()); err != nil {
		if os.IsNotExist(err) {
			return errors.New(obsidian.NoteDoesNotExistError)
		}
		return errors.New(obsidian.VaultWriteError)
	}
	return nil
}
