//go:build cgo

package codeanchor

import (
	"crypto/sha256"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

var tsWorkspacePackageIgnoredDirs = func() map[string]struct{} {
	dirs := make(map[string]struct{})
	for _, name := range ignore.DefaultIgnoreDirnames() {
		dirs[name] = struct{}{}
	}
	return dirs
}()

type tsPackageManifest struct {
	name    string
	dir     string
	exports any
	imports any
	types   string
	module  string
	main    string
	path    string
	stamp   [sha256.Size]byte
}

func (r *tsModuleResolver) resolveWorkspacePackage(specifier string) (string, bool) {
	name, subpath := splitTSPackageSpecifier(specifier)
	if name == "" {
		return "", false
	}
	manifest, ok := r.workspacePackage(name)
	if !ok {
		return "", false
	}
	exportKey := "."
	if subpath != "" {
		exportKey = "./" + subpath
	}
	for _, target := range resolveTSPackageExportTargets(manifest.exports, exportKey) {
		if resolved, ok := r.resolveFile(filepath.Join(manifest.dir, filepath.FromSlash(target))); ok {
			return resolved, true
		}
	}
	if manifest.exports != nil {
		return "", false
	}
	if subpath != "" {
		for _, target := range []string{subpath, filepath.Join("src", subpath)} {
			if resolved, ok := r.resolveFile(filepath.Join(manifest.dir, filepath.FromSlash(target))); ok {
				return resolved, true
			}
		}
		return "", false
	}
	for _, target := range []string{manifest.types, manifest.module, manifest.main, "src/index", "index"} {
		if strings.TrimSpace(target) == "" {
			continue
		}
		if resolved, ok := r.resolveFile(filepath.Join(manifest.dir, filepath.FromSlash(target))); ok {
			return resolved, true
		}
	}
	return "", false
}

func resolveTSPackageExportTargets(value any, key string) []string {
	if key != "." {
		mapped, ok := value.(map[string]any)
		if !ok || !hasTSPackageSubpathKeys(mapped) {
			return nil
		}
	}
	return resolveTSPackageTargets(value, key)
}

func (r *tsModuleResolver) resolvePackageImport(importerDir, specifier string) (string, bool) {
	manifest, ok := r.nearestManifest(importerDir)
	if !ok {
		return "", false
	}
	for _, target := range resolveTSPackageTargets(manifest.imports, specifier) {
		if strings.HasPrefix(target, ".") {
			if resolved, ok := r.resolveFile(filepath.Join(manifest.dir, filepath.FromSlash(target))); ok {
				return resolved, true
			}
			continue
		}
		if resolved, ok := r.resolveWorkspacePackage(target); ok {
			return resolved, true
		}
	}
	return "", false
}

func (r *tsModuleResolver) workspacePackage(name string) (tsPackageManifest, bool) {
	r.packageMu.RLock()
	scanned := r.packageScanned
	dirty := r.packageDirty
	r.packageMu.RUnlock()
	if !scanned || dirty || r.requestedPackageStale(name) {
		r.scanWorkspacePackages()
	}
	r.packageMu.RLock()
	manifest, ok := r.packages[name]
	ambiguous := r.ambiguous[name]
	r.packageMu.RUnlock()
	if ok && !ambiguous {
		return manifest, true
	}
	return tsPackageManifest{}, false
}

func (r *tsModuleResolver) requestedPackageStale(name string) bool {
	r.packageMu.RLock()
	known := r.packageFiles[name]
	files := make(map[string][sha256.Size]byte, len(known))
	for path, stamp := range known {
		files[path] = stamp
	}
	r.packageMu.RUnlock()
	for path, stamp := range files {
		data, err := os.ReadFile(path)
		if err != nil || sha256.Sum256(data) != stamp {
			return true
		}
	}
	return false
}

func (r *tsModuleResolver) scanWorkspacePackages() {
	r.packageRefreshMu.Lock()
	defer r.packageRefreshMu.Unlock()
	packages := make(map[string]tsPackageManifest)
	ambiguous := make(map[string]bool)
	packageFiles := make(map[string]map[string][sha256.Size]byte)
	_ = filepath.WalkDir(r.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != r.root && isTSWorkspacePackageIgnoredDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "package.json" {
			return nil
		}
		manifest, ok := readTSPackageManifest(path)
		if ok && manifest.name != "" {
			if packageFiles[manifest.name] == nil {
				packageFiles[manifest.name] = make(map[string][sha256.Size]byte)
			}
			packageFiles[manifest.name][path] = manifest.stamp
		}
		if ok && manifest.name != "" {
			if existing, exists := packages[manifest.name]; exists && existing.dir != manifest.dir {
				ambiguous[manifest.name] = true
			} else {
				packages[manifest.name] = manifest
			}
		}
		return nil
	})
	r.packageMu.Lock()
	r.packages = packages
	r.ambiguous = ambiguous
	r.packageFiles = packageFiles
	r.packageScanned = true
	r.packageDirty = false
	r.packageScanCount++
	r.packageMu.Unlock()
}

func (r *tsModuleResolver) nearestManifest(start string) (tsPackageManifest, bool) {
	cur := start
	for r.withinRoot(cur) {
		if manifest, ok := readTSPackageManifest(filepath.Join(cur, "package.json")); ok {
			return manifest, true
		}
		if cur == r.root {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return tsPackageManifest{}, false
}

func readTSPackageManifest(path string) (tsPackageManifest, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return tsPackageManifest{}, false
	}
	var raw struct {
		Name    string `json:"name"`
		Exports any    `json:"exports"`
		Imports any    `json:"imports"`
		Types   string `json:"types"`
		Typings string `json:"typings"`
		Module  string `json:"module"`
		Main    string `json:"main"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return tsPackageManifest{}, false
	}
	types := raw.Types
	if types == "" {
		types = raw.Typings
	}
	return tsPackageManifest{
		name: strings.TrimSpace(raw.Name), dir: filepath.Dir(path), exports: raw.Exports,
		imports: raw.Imports, types: types, module: raw.Module, main: raw.Main,
		path: path, stamp: sha256.Sum256(data),
	}, true
}

func splitTSPackageSpecifier(specifier string) (string, string) {
	parts := strings.Split(strings.Trim(specifier, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	count := 1
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 2 {
			return "", ""
		}
		count = 2
	}
	return strings.Join(parts[:count], "/"), strings.Join(parts[count:], "/")
}

func resolveTSPackageTargets(value any, key string) []string {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			return []string{typed}
		}
	case []any:
		var out []string
		for _, item := range typed {
			out = append(out, resolveTSPackageTargets(item, key)...)
		}
		return out
	case map[string]any:
		if hasTSPackageSubpathKeys(typed) {
			if target, ok := typed[key]; ok {
				return resolveTSPackageTargets(target, key)
			}
			patterns := make([]string, 0, len(typed))
			for pattern := range typed {
				if strings.Contains(pattern, "*") {
					patterns = append(patterns, pattern)
				}
			}
			sort.Strings(patterns)
			bestScore := tsPatternScore{-1, -1, -1}
			var bestTargets []string
			for _, pattern := range patterns {
				capture, ok := matchTSPackagePattern(pattern, key)
				if !ok {
					continue
				}
				targets := resolveTSPackageTargets(typed[pattern], key)
				if len(targets) > 0 {
					for i := range targets {
						targets[i] = strings.ReplaceAll(targets[i], "*", capture)
					}
					score := scoreTSPattern(pattern)
					switch compareTSPatternScore(score, bestScore) {
					case 1:
						bestScore, bestTargets = score, targets
					case 0:
						if !reflect.DeepEqual(bestTargets, targets) {
							return nil
						}
					}
				}
			}
			return bestTargets
		}
		var out []string
		// Value relationships prefer runtime/source entry points; declarations are
		// only a fallback when a package exposes no supported runtime condition.
		// The fixed order keeps resolution deterministic across JSON map iteration.
		// These conditions describe source and standard Node module entry points
		// that Rhizome can resolve without knowing a bundler's environment.
		// Custom conditions and browser-only branches intentionally remain
		// unresolved rather than creating plausible but incorrect graph edges.
		for _, condition := range []string{"source", "import", "node", "require", "default", "types"} {
			if target, ok := typed[condition]; ok {
				out = append(out, resolveTSPackageTargets(target, key)...)
			}
		}
		return out
	}
	return nil
}

func isTSWorkspacePackageIgnoredDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	_, ignored := tsWorkspacePackageIgnoredDirs[name]
	return ignored
}

func hasTSPackageSubpathKeys(values map[string]any) bool {
	for key := range values {
		if strings.HasPrefix(key, ".") || strings.HasPrefix(key, "#") {
			return true
		}
	}
	return false
}

func matchTSPackagePattern(pattern, key string) (string, bool) {
	star := strings.Index(pattern, "*")
	if star < 0 {
		return "", pattern == key
	}
	prefix, suffix := pattern[:star], pattern[star+1:]
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) || len(key) < len(prefix)+len(suffix) {
		return "", false
	}
	return key[len(prefix) : len(key)-len(suffix)], true
}
