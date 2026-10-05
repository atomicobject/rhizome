//go:build cgo

package codeanchor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

type tsExternalVersionManifest struct {
	path   string
	ranges map[string]tsExternalDeclaredVersion
}

type tsExternalDeclaredVersion struct {
	rangeValue string
	scope      ExternalVersionScope
}

func (t *TSIndexer) tsExternalVersionProvenance(fileDir, module string) *ExternalVersionProvenance {
	packageName, _ := splitTSPackageSpecifier(module)
	if packageName == "" || strings.HasPrefix(module, "node:") {
		return nil
	}
	manifest := t.tsExternalVersionManifest(fileDir)
	declared, ok := manifest.ranges[packageName]
	if !ok {
		return nil
	}
	return &ExternalVersionProvenance{ManifestPath: manifest.path, DeclaredRange: declared.rangeValue, Scope: declared.scope}
}

// tsExternalVersionManifest caches the nearest manifest once per importer
// directory. All bindings in a file then perform map lookups only.
func (t *TSIndexer) tsExternalVersionManifest(fileDir string) tsExternalVersionManifest {
	dir := paths.ResolveSymlinks(fileDir).String()
	t.externalVersionMu.Lock()
	defer t.externalVersionMu.Unlock()
	if cached, ok := t.externalVersionCache[dir]; ok {
		return cached
	}
	result := tsExternalVersionManifest{ranges: make(map[string]tsExternalDeclaredVersion)}
	for current := dir; current != ""; current = filepath.Dir(current) {
		if t.resolver == nil || !t.resolver.withinRoot(current) {
			break
		}
		manifestPath := filepath.Join(current, "package.json")
		if data, err := os.ReadFile(manifestPath); err == nil {
			var raw struct {
				Dependencies         map[string]string `json:"dependencies"`
				DevDependencies      map[string]string `json:"devDependencies"`
				PeerDependencies     map[string]string `json:"peerDependencies"`
				OptionalDependencies map[string]string `json:"optionalDependencies"`
			}
			if json.Unmarshal(data, &raw) == nil {
				if vault, err := paths.NewVaultPaths(t.repoRoot); err == nil {
					if rel, err := vault.RelCodeStrict(manifestPath); err == nil {
						result.path = rel.String()
					}
				}
				appendTSDeclaredVersions(result.ranges, raw.Dependencies, ExternalVersionDependency)
				appendTSDeclaredVersions(result.ranges, raw.DevDependencies, ExternalVersionDevDependency)
				appendTSDeclaredVersions(result.ranges, raw.PeerDependencies, ExternalVersionPeerDependency)
				appendTSDeclaredVersions(result.ranges, raw.OptionalDependencies, ExternalVersionOptionalDependency)
			}
			break
		}
		if current == t.repoRoot || filepath.Dir(current) == current {
			break
		}
	}
	t.externalVersionCache[dir] = result
	return result
}

func appendTSDeclaredVersions(dst map[string]tsExternalDeclaredVersion, src map[string]string, scope ExternalVersionScope) {
	for name, declaredRange := range src {
		if _, exists := dst[name]; !exists {
			dst[name] = tsExternalDeclaredVersion{rangeValue: declaredRange, scope: scope}
		}
	}
}
