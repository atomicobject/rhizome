//go:build cgo

package codeanchor

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type tsConfigResolution struct {
	baseDir string
	paths   map[string][]string
}

type tsConfigCacheEntry struct {
	resolution   *tsConfigResolution
	dependencies map[string]tsConfigDependencyStamp
}

type tsConfigDependencyStamp struct {
	hash    [sha256.Size]byte
	size    int64
	modTime time.Time
}

type tsConfigMatch uint8

const (
	tsConfigMatchNone tsConfigMatch = iota
	tsConfigMatchFound
	tsConfigMatchAmbiguous
)

func (r *tsModuleResolver) resolveConfigPaths(importerDir, specifier string) ([]string, tsConfigMatch) {
	configPath := r.nearestTSConfig(importerDir)
	if configPath == "" {
		return nil, tsConfigMatchNone
	}
	config := r.loadTSConfig(configPath, make(map[string]bool))
	if config == nil {
		return nil, tsConfigMatchNone
	}
	patterns := make([]string, 0, len(config.paths))
	for pattern := range config.paths {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	bestScore := tsPatternScore{-1, -1, -1}
	var bestTargets []string
	for _, pattern := range patterns {
		capture, ok := matchTSPackagePattern(pattern, specifier)
		if !ok {
			continue
		}
		score := scoreTSPattern(pattern)
		targets := config.paths[pattern]
		out := make([]string, 0, len(targets))
		for _, target := range targets {
			out = append(out, strings.ReplaceAll(target, "*", capture))
		}
		switch compareTSPatternScore(score, bestScore) {
		case 1:
			bestScore, bestTargets = score, out
		case 0:
			if !reflect.DeepEqual(bestTargets, out) {
				return nil, tsConfigMatchAmbiguous
			}
		}
	}
	if bestScore.exact >= 0 {
		return bestTargets, tsConfigMatchFound
	}
	if config.baseDir != "" {
		return []string{filepath.Join(config.baseDir, filepath.FromSlash(specifier))}, tsConfigMatchFound
	}
	return nil, tsConfigMatchNone
}

func (r *tsModuleResolver) nearestTSConfig(start string) string {
	cur := start
	for r.withinRoot(cur) {
		for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
			candidate := filepath.Join(cur, name)
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				return candidate
			}
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
	return ""
}

func (r *tsModuleResolver) loadTSConfig(path string, visiting map[string]bool) *tsConfigResolution {
	entry := r.loadTSConfigEntry(path, visiting)
	return entry.resolution
}

func (r *tsModuleResolver) loadTSConfigEntry(path string, visiting map[string]bool) tsConfigCacheEntry {
	path = filepath.Clean(path)
	if !r.withinRoot(path) || visiting[path] {
		return tsConfigCacheEntry{}
	}
	r.configMu.Lock()
	if cached, ok := r.configs[path]; ok {
		r.configMu.Unlock()
		if refreshed, current := tsConfigDependenciesCurrent(cached.dependencies); current {
			cached.dependencies = refreshed
			r.configMu.Lock()
			if currentEntry, present := r.configs[path]; present && currentEntry.resolution == cached.resolution {
				r.configs[path] = cached
			}
			r.configMu.Unlock()
			return cached
		}
	} else {
		r.configMu.Unlock()
	}
	visiting[path] = true
	defer delete(visiting, path)

	data, err := os.ReadFile(path)
	if err != nil {
		return tsConfigCacheEntry{}
	}
	var raw struct {
		Extends         string `json:"extends"`
		CompilerOptions struct {
			BaseURL *string             `json:"baseUrl"`
			Paths   map[string][]string `json:"paths"`
		} `json:"compilerOptions"`
	}
	if json.Unmarshal(cleanTSConfigJSON(data), &raw) != nil {
		return tsConfigCacheEntry{}
	}
	dir := filepath.Dir(path)
	var config tsConfigResolution
	dependencies := map[string]tsConfigDependencyStamp{path: newTSConfigDependencyStamp(path, data)}
	if strings.HasPrefix(raw.Extends, ".") || filepath.IsAbs(raw.Extends) {
		extended := raw.Extends
		if !filepath.IsAbs(extended) {
			extended = filepath.Join(dir, filepath.FromSlash(extended))
		}
		if info, err := os.Stat(extended); err != nil || !info.Mode().IsRegular() {
			extended += ".json"
		}
		if parent := r.loadTSConfigEntry(extended, visiting); parent.resolution != nil {
			config.baseDir = parent.resolution.baseDir
			config.paths = cloneTSConfigPaths(parent.resolution.paths)
			for dependency, stamp := range parent.dependencies {
				dependencies[dependency] = stamp
			}
		}
	}
	if raw.CompilerOptions.BaseURL != nil {
		config.baseDir = filepath.Clean(filepath.Join(dir, filepath.FromSlash(*raw.CompilerOptions.BaseURL)))
	}
	if raw.CompilerOptions.Paths != nil {
		config.paths = make(map[string][]string, len(raw.CompilerOptions.Paths))
		baseDir := config.baseDir
		if baseDir == "" {
			baseDir = dir
		}
		for pattern, targets := range raw.CompilerOptions.Paths {
			for _, target := range targets {
				config.paths[pattern] = append(config.paths[pattern], filepath.Join(baseDir, filepath.FromSlash(target)))
			}
		}
	}
	entry := tsConfigCacheEntry{resolution: &config, dependencies: dependencies}
	r.configMu.Lock()
	r.configs[path] = entry
	r.configMu.Unlock()
	return entry
}

func tsConfigDependenciesCurrent(dependencies map[string]tsConfigDependencyStamp) (map[string]tsConfigDependencyStamp, bool) {
	if len(dependencies) == 0 {
		return nil, false
	}
	refreshed := make(map[string]tsConfigDependencyStamp, len(dependencies))
	for path, want := range dependencies {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return nil, false
		}
		if info.Size() == want.size && info.ModTime().Equal(want.modTime) {
			refreshed[path] = want
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || sha256.Sum256(data) != want.hash {
			return nil, false
		}
		refreshed[path] = tsConfigDependencyStamp{hash: want.hash, size: info.Size(), modTime: info.ModTime()}
	}
	return refreshed, true
}

func newTSConfigDependencyStamp(path string, data []byte) tsConfigDependencyStamp {
	stamp := tsConfigDependencyStamp{hash: sha256.Sum256(data), size: int64(len(data))}
	if info, err := os.Stat(path); err == nil {
		stamp.size = info.Size()
		stamp.modTime = info.ModTime()
	}
	return stamp
}

type tsPatternScore struct {
	exact  int
	prefix int
	suffix int
}

func scoreTSPattern(pattern string) tsPatternScore {
	star := strings.Index(pattern, "*")
	if star < 0 {
		return tsPatternScore{exact: 1, prefix: len(pattern), suffix: 0}
	}
	return tsPatternScore{exact: 0, prefix: star, suffix: len(pattern) - star - 1}
}

func compareTSPatternScore(left, right tsPatternScore) int {
	if left.exact != right.exact {
		return signTSInt(left.exact - right.exact)
	}
	if left.prefix != right.prefix {
		return signTSInt(left.prefix - right.prefix)
	}
	return signTSInt(left.suffix - right.suffix)
}

func signTSInt(value int) int {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}

func cloneTSConfigPaths(input map[string][]string) map[string][]string {
	if input == nil {
		return nil
	}
	out := make(map[string][]string, len(input))
	for key, values := range input {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func cleanTSConfigJSON(input []byte) []byte {
	var out bytes.Buffer
	inString := false
	escaped := false
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(input) && input[i+1] == '/' {
			i += 2
			for i < len(input) && input[i] != '\n' {
				i++
			}
			out.WriteByte('\n')
			continue
		}
		if ch == '/' && i+1 < len(input) && input[i+1] == '*' {
			i += 2
			for i+1 < len(input) && !(input[i] == '*' && input[i+1] == '/') {
				i++
			}
			i++
			continue
		}
		out.WriteByte(ch)
	}
	withoutComments := out.Bytes()
	out.Reset()
	inString = false
	escaped = false
	for i := 0; i < len(withoutComments); i++ {
		ch := withoutComments[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == ',' {
			next := i + 1
			for next < len(withoutComments) && (withoutComments[next] == ' ' || withoutComments[next] == '\t' || withoutComments[next] == '\r' || withoutComments[next] == '\n') {
				next++
			}
			if next < len(withoutComments) && (withoutComments[next] == '}' || withoutComments[next] == ']') {
				continue
			}
		}
		out.WriteByte(ch)
	}
	return out.Bytes()
}
