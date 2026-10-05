package actions

import (
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func SearchNotes(vault obsidian.VaultManager, note obsidian.NoteReader, uri obsidian.UriManager, fuzzyFinder obsidian.FuzzyFinderManager) error {
	vaultName, err := vault.DefaultName()
	if err != nil {
		return err
	}

	vaultDef, err := vault.Definition()
	if err != nil {
		return err
	}

	notes, err := note.GetNotesList(vaultDef)
	if err != nil {
		return err
	}

	index, err := fuzzyFinder.Find(notes, func(i int) string {
		return notes[i]
	})

	if err != nil {
		return err
	}

	obsidianUri := uri.Construct(ObsOpenUrl, map[string]string{
		"file":  notes[index],
		"vault": vaultName,
	})

	err = uri.Execute(obsidianUri)
	if err != nil {
		return err
	}

	return nil
}
