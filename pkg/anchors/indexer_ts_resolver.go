//go:build cgo

package codeanchor

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/atomicobject/rhizome/pkg/codefile"
	"github.com/atomicobject/rhizome/pkg/paths"
)

type tsModuleResolver struct {
	root string

	configMu sync.Mutex
	configs  map[string]tsConfigCacheEntry

	packageMu        sync.RWMutex
	packageRefreshMu sync.Mutex
	packages         map[string]tsPackageManifest
	ambiguous        map[string]bool
	packageFiles     map[string]map[string][32]byte
	packageScanned   bool
	packageDirty     bool
	packageScanCount int
}

func (r *tsModuleResolver) invalidateModuleMetadata(path string) {
	if r == nil {
		return
	}
	base := strings.ToLower(filepath.Base(path))
	if path == "" || base == "package.json" {
		r.packageMu.Lock()
		r.packageDirty = true
		r.packageMu.Unlock()
	}
	// Extends targets may use any JSON filename, so every JSON event invalidates
	// config resolution. Only package.json dirties the separately cached workspace
	// registry; editing an arbitrary extended config must not trigger a repo walk.
	if path == "" || strings.EqualFold(filepath.Ext(path), ".json") {
		r.configMu.Lock()
		r.configs = make(map[string]tsConfigCacheEntry)
		r.configMu.Unlock()
	}
}

func newTSModuleResolver(root string) *tsModuleResolver {
	return &tsModuleResolver{
		root:         paths.ResolveSymlinks(root).String(),
		packages:     make(map[string]tsPackageManifest),
		ambiguous:    make(map[string]bool),
		packageFiles: make(map[string]map[string][32]byte),
		configs:      make(map[string]tsConfigCacheEntry),
	}
}

func (r *tsModuleResolver) resolve(importerDir, specifier string) (string, bool) {
	if r == nil {
		return "", false
	}
	importerDir = paths.ResolveSymlinks(importerDir).String()
	specifier = strings.TrimSpace(specifier)
	if importerDir == "" || specifier == "" || !r.withinRoot(importerDir) {
		return "", false
	}
	var result string
	var ok bool
	switch {
	case filepath.IsAbs(specifier):
		result, ok = r.resolveFile(specifier)
	case strings.HasPrefix(specifier, "."):
		result, ok = r.resolveFile(filepath.Join(importerDir, specifier))
	case strings.HasPrefix(specifier, "#"):
		result, ok = r.resolvePackageImport(importerDir, specifier)
	default:
		candidates, configMatch := r.resolveConfigPaths(importerDir, specifier)
		if configMatch == tsConfigMatchAmbiguous {
			return "", false
		}
		if len(candidates) > 0 {
			for _, candidate := range candidates {
				if result, ok = r.resolveFile(candidate); ok {
					break
				}
			}
		}
		if !ok {
			result, ok = r.resolveWorkspacePackage(specifier)
		}
	}
	if ok && !r.withinRoot(result) {
		result, ok = "", false
	}
	return result, ok
}

func (r *tsModuleResolver) resolveFile(base string) (string, bool) {
	base = filepath.Clean(base)
	if !r.withinRoot(base) {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(base))
	var candidates []string
	switch ext {
	case ".js":
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		candidates = []string{stem + ".ts", stem + ".tsx", base}
	case ".jsx":
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		candidates = []string{stem + ".tsx", base}
	case ".mjs":
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		candidates = []string{stem + ".mts", base}
	case ".cjs":
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		candidates = []string{stem + ".cts", base}
	case ".ts", ".tsx", ".mts", ".cts":
		candidates = []string{base}
	default:
		for _, candidateExt := range codefile.TypeScriptJavaScriptExtensions() {
			candidates = append(candidates, base+candidateExt)
		}
	}
	for _, candidate := range candidates {
		if resolved, ok := r.regularFile(candidate); ok {
			return resolved, true
		}
	}
	if info, err := os.Stat(base); err == nil && info.IsDir() {
		for _, candidateExt := range codefile.TypeScriptJavaScriptExtensions() {
			if resolved, ok := r.regularFile(filepath.Join(base, "index"+candidateExt)); ok {
				return resolved, true
			}
		}
	}
	return "", false
}

func (r *tsModuleResolver) regularFile(candidate string) (string, bool) {
	if !r.withinRoot(candidate) {
		return "", false
	}
	info, err := os.Stat(candidate)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	resolved := paths.ResolveSymlinks(candidate).String()
	if resolved == "" || !r.withinRoot(resolved) {
		return "", false
	}
	return resolved, true
}

func (r *tsModuleResolver) withinRoot(candidate string) bool {
	if r == nil || r.root == "" || candidate == "" {
		return false
	}
	rel, err := filepath.Rel(r.root, filepath.Clean(candidate))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
