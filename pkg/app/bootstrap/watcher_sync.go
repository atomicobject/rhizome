package bootstrap

import (
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/codefile"
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
