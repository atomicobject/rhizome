package actions

import (
	"errors"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// FileInfo represents the information about a file
type FileInfo struct {
	Frontmatter map[string]interface{}
	Tags        []string
}

// GetFileInfo retrieves the frontmatter and tags from a file.
func GetFileInfo(vault obsidian.VaultManager, note obsidian.NoteReader, filePath string) (*FileInfo, error) {
	vaultDef, err := vault.Definition()
	if err != nil {
		return nil, err
	}

	vaultPath := vaultDef.BasePath()
	if vaultPath == "" {
		return nil, errors.New(obsidian.RhizomeVaultPathInvalidError)
	}
	vaultPaths, err := paths.NewVaultPaths(vaultPath)
	if err != nil || vaultPaths.Root() == "" {
		return nil, errors.New(obsidian.RhizomeVaultPathInvalidError)
	}
	rel, _, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, filePath)
	if err != nil || rel == "" {
		return nil, errors.New(obsidian.NoteDoesNotExistError)
	}

	fact, ok := NoteFactsFromReader(note).LookupFact(rel.String())
	if !ok {
		if _, err := note.GetContents(vaultDef, rel.String()); err != nil {
			return nil, err
		}
		return nil, errors.New("current note metadata is unavailable; run rzm index")
	}

	return &FileInfo{
		Frontmatter: fact.Frontmatter,
		Tags:        fact.Tags,
	}, nil
}
