package actions

import (
	"fmt"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

const (
	CodeExplainNote = "note"
	CodeExplainFile = "code"
)

// CodeExplainInput is one path classified by the configured note and code
// ownership rules.
type CodeExplainInput struct {
	Path       string
	Kind       string
	Descriptor noteformat.Descriptor
}

// ClassifyCodeExplainInputs applies the vault's exclusive note/code ownership
// rules before the explain command reads notes or opens the code index.
func ClassifyCodeExplainInputs(vaultDef obsidian.VaultDefinition, runtime noteformat.Runtime, rawPaths []string) ([]CodeExplainInput, error) {
	vaultPaths, err := paths.NewVaultPaths(vaultDef.BasePath())
	if err != nil || vaultPaths.Root() == "" {
		return nil, fmt.Errorf("resolve configured ownership: invalid vault path %q", vaultDef.BasePath())
	}
	selector, err := noteownership.CompileSelector(noteownership.SelectorInput{
		VaultDefinition: vaultDef,
		Registry:        runtime.Registry(),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(vaultPaths.Root())},
		CodeLanguage: func(ref paths.CodePathRef) codeanchor.Lang {
			return codeanchor.Lang(coderefs.DetectLanguage(ref.Rel.String()))
		},
	})
	if err != nil {
		return nil, err
	}

	inputs := make([]CodeExplainInput, 0, len(rawPaths))
	for _, raw := range rawPaths {
		rel, err := vaultPaths.RelStrict(raw)
		if err != nil || rel == "" {
			return nil, fmt.Errorf("path %q has no configured ownership", raw)
		}
		selection, err := selector.Select(rel)
		if err != nil {
			return nil, err
		}
		switch selection.Owner {
		case notediscovery.Note:
			provider, found := runtime.Provider(selection.Provider)
			if !found {
				return nil, fmt.Errorf("configured note provider %q is unavailable", selection.Provider)
			}
			inputs = append(inputs, CodeExplainInput{Path: raw, Kind: CodeExplainNote, Descriptor: provider.Descriptor()})
		case notediscovery.Code:
			inputs = append(inputs, CodeExplainInput{Path: raw, Kind: CodeExplainFile})
		default:
			return nil, fmt.Errorf("path %q has no configured ownership", raw)
		}
	}
	return inputs, nil
}
