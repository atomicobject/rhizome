package codeintel

import (
	"fmt"
	"os"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ReadAuthoredMarkdownAnchors confines source reads to the vault and parses
// only notes admitted to the configured Markdown provider.
func ReadAuthoredMarkdownAnchors(vault obsidian.VaultDefinition, formats noteformat.Runtime, notePath string) ([]codeanchor.Anchor, *diag.Warning) {
	warn := func(code, message string) ([]codeanchor.Anchor, *diag.Warning) {
		return nil, &diag.Warning{Code: code, Message: message, Path: notePath}
	}
	vaultPaths, err := paths.NewVaultPaths(vault.BasePath())
	if err != nil {
		return warn("invalid_note_path", err.Error())
	}
	rel, abs, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, notePath)
	if err != nil {
		return warn("invalid_note_path", fmt.Sprintf("note path must be inside the vault: %v", err))
	}
	if _, err := vaultPaths.RelStrict(abs.String()); err != nil {
		return warn("invalid_note_path", fmt.Sprintf("note path must be inside the vault: %v", err))
	}
	notePath = rel.String()
	if len(formats.Registry().IDs()) == 0 {
		return warn("note_format_unavailable", "note format runtime is unavailable")
	}
	selector, err := noteownership.CompileSelector(noteownership.SelectorInput{
		VaultDefinition: vault,
		Registry:        formats.Registry(),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(vaultPaths.Root())},
		CodeLanguage: func(ref paths.CodePathRef) codeanchor.Lang {
			return codeanchor.Lang(coderefs.DetectLanguage(ref.Rel.String()))
		},
	})
	if err != nil {
		return warn("note_format_unavailable", err.Error())
	}
	selection, err := selector.Select(paths.RelPath(rel))
	if err != nil {
		return warn("note_format_unavailable", err.Error())
	}
	if selection.Owner != notediscovery.Note {
		return warn("note_format_unavailable", fmt.Sprintf("path %q is not a configured note", notePath))
	}
	if selection.Provider != noteformat.FormatID("markdown") {
		return warn("code_anchor_fallback_unsupported_format", fmt.Sprintf("authored code-anchor fallback is not supported for note format %q", selection.Provider))
	}
	content, err := os.ReadFile(abs.String())
	if err != nil {
		return warn("note_read_failed", err.Error())
	}
	note, err := codeanchor.ParseNote(notePath, string(content))
	if err != nil {
		return warn("note_anchor_parse_failed", err.Error())
	}
	return note.DefinedAnchors, nil
}
