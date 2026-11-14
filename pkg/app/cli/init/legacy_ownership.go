package init

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
)

// foldLegacyStarterFiles transfers only ownership proven by the raw hash
// recorded before generated-files.yml. Targets come from the starter registry;
// family and filename determine the flat path, even for a retired starter.
func (g *generatedFiles) foldLegacyStarterFiles(projectRoot string, fingerprints map[string]string) error {
	if len(fingerprints) == 0 {
		return nil
	}
	registry, err := loadStarterTemplateMetadataRegistry()
	if err != nil {
		return err
	}
	targets := map[string]generatedFile{}
	for template := range registry {
		files, err := starterModuleFiles(template)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.legacy == "" {
				continue
			}
			flatDir := assetroots.FlatDir(file.kind)
			family := path.Base(flatDir)
			name := strings.TrimPrefix(file.legacy, flatDir+"/")
			fingerprint := contentFingerprint
			if file.kind == assetroots.Ontology {
				fingerprint = ontologyFingerprint
			}
			targets[family+":"+name] = generatedFile{rel: file.legacy, fingerprint: fingerprint}
		}
		docs, err := loadStarterTemplates(template)
		if err != nil {
			return err
		}
		for _, file := range docs {
			rel := filepath.ToSlash(file.Path)
			targets["docs:"+rel] = generatedFile{rel: rel, fingerprint: contentFingerprint}
		}
	}
	for key, hash := range fingerprints {
		_, family, name, ok := parseStarterFingerprintKey(key)
		if !ok {
			continue
		}
		if family == "queryRecipes" {
			family = "query-recipes"
		}
		file, known := targets[family+":"+name]
		if !known {
			continue
		}
		if g.Written[file.rel] != "" {
			continue
		}
		if g.Declined[file.rel] != "" {
			continue
		}
		data, exists, err := readIfExists(filepath.Join(projectRoot, filepath.FromSlash(file.rel)))
		if err != nil {
			return err
		}
		if exists && fingerprintStarterAsset(data) == hash {
			g.Written[file.rel] = file.fingerprint(data)
		}
	}
	return nil
}
