package bootstrap

import (
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func classifyInternalChange(rel string) internalChangeEffect {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	switch {
	case rel == ".rhizome/config.yml":
		return internalChangeEffect{reloadVaultDef: true, refreshMeta: true, refreshOntology: true, invalidateCapabilities: true}
	case rel == ".rhizome/ignore", rel == ".obsidianignore":
		return internalChangeEffect{refreshMeta: true, refreshOntology: true}
	case strings.HasPrefix(rel, ".rhizome/ontology/") && filepath.Ext(rel) == ".graphql":
		return internalChangeEffect{refreshOntology: true, invalidateCapabilities: true}
	case strings.HasPrefix(rel, ".rhizome/query-recipes/") && isYAMLPath(rel):
		return internalChangeEffect{invalidateQueryRecipes: true, invalidateCapabilities: true}
	default:
		return internalChangeEffect{}
	}
}

func (effect internalChangeEffect) any() bool {
	return effect.reloadVaultDef || effect.refreshMeta || effect.refreshOntology || effect.invalidateQueryRecipes || effect.invalidateCapabilities
}

func isYAMLPath(rel string) bool {
	ext := filepath.Ext(filepath.ToSlash(rel))
	return ext == ".yaml" || ext == ".yml"
}

func loadWatcherVaultDefinition(vaultPath string, current obsidian.VaultDefinition) (obsidian.VaultDefinition, error) {
	loaded, err := obsidian.LoadDefinitionFromPath(vaultPath)
	if err != nil {
		return current, err
	}
	if loaded.Name == "" {
		loaded.Name = current.Name
	}
	return loaded, nil
}

// detectCodeLang determines the code language from a file path extension.
func detectCodeLang(path string) codeanchor.Lang {
	ext := strings.ToLower(filepath.Ext(path))
	if codefile.IsTypeScriptJavaScriptExtension(ext) {
		return codeanchor.LangTS
	}
	switch ext {
	case ".py":
		return codeanchor.LangPy
	case ".go":
		return codeanchor.LangGo
	case ".cs":
		return codeanchor.LangCs
	case ".php":
		return codeanchor.LangPhp
	default:
		return ""
	}
}

// ResolveRoots converts relative paths to absolute paths based on the vault path.
func ResolveRoots(vaultPath string, roots []string) []string {
	var result []string
	for _, root := range roots {
		if root == "" {
			continue
		}
		absRoot := root
		if !filepath.IsAbs(root) {
			absRoot = filepath.Join(vaultPath, root)
		}
		result = append(result, absRoot)
	}
	return result
}
