package operations

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sourceownership"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"strconv"
	"strings"
)

type CreateParams struct {
	NoteName        string
	ShouldAppend    bool
	ShouldOverwrite bool
	Content         string
	ShouldOpen      bool
}

func CreateNote(vault repoconfig.VaultManager, uri obsidian.UriManager, params CreateParams) error {
	vaultName, err := vault.DefaultName()
	if err != nil {
		return err
	}

	root, err := vault.Path()
	if err != nil {
		return err
	}
	if err := sourceownership.Check(root, []string{params.NoteName, paths.EnsureNoteExtension(params.NoteName, ".md").String()}); err != nil {
		return err
	}
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return err
	}
	destination, err := vaultPaths.RelPhysicalStrict(paths.ResolveAgainst(vaultPaths.Root(), params.NoteName))
	if err != nil {
		return err
	}
	if destination.String() == "" {
		return fmt.Errorf("note name must name a file")
	}
	normalizedContent := NormalizeContent(params.Content)

	obsidianUri := uri.Construct(ObsCreateUrl, map[string]string{
		"vault":     vaultName,
		"append":    strconv.FormatBool(params.ShouldAppend),
		"overwrite": strconv.FormatBool(params.ShouldOverwrite),
		"content":   normalizedContent,
		"file":      destination.String(),
		"silent":    strconv.FormatBool(!params.ShouldOpen),
	})

	if err := uri.Execute(obsidianUri); err != nil {
		return err
	}

	return nil
}

func NormalizeContent(content string) string {
	replacer := strings.NewReplacer(
		"\\n", "\n",
		"\\r", "\r",
		"\\t", "\t",
		"\\\\", "\\",
		"\\\"", "\"",
		"\\'", "'",
	)
	return replacer.Replace(content)
}
