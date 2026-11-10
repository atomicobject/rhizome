package bootstrap

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
)

func classifyInternalChange(rel string) internalChangeEffect {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	switch {
	case rel == ".rhizome/config.yml":
		return internalChangeEffect{reloadVaultDef: true, refreshMeta: true, refreshOntology: true, invalidateCapabilities: true}
	case rel == ".rhizome/ignore", rel == ".obsidianignore":
		return internalChangeEffect{refreshMeta: true, refreshOntology: true}
	case assetroots.IsAssetRootOrAncestor(rel, assetroots.Ontology):
		return internalChangeEffect{refreshOntology: true, invalidateCapabilities: true}
	case assetroots.IsAssetPath(rel, assetroots.Ontology) && filepath.Ext(rel) == ".graphql":
		return internalChangeEffect{refreshOntology: true, invalidateCapabilities: true}
	case assetroots.IsAssetPath(rel, assetroots.Recipes) && isYAMLPath(rel):
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
