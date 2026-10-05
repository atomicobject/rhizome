package obsidian

import (
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// VaultFileIndex resolves wikilink paths with Obsidian's rules
// (MetadataCache.getLinkpathDest in Obsidian 1.14): every file outside
// dot-folders is a target, names compare case-insensitively, only `.md` is
// implied, and several same-named files resolve by preferring the source
// note's folder and then the shortest path. Ignore rules exclude files from
// indexing and search, never from link resolution, so they do not apply here.
type VaultFileIndex struct {
	byName map[string][]string
}

// NewVaultFileIndex indexes vault-relative, slash-separated file paths.
func NewVaultFileIndex(files []string) *VaultFileIndex {
	index := &VaultFileIndex{byName: make(map[string][]string, len(files))}
	for _, file := range files {
		file = filepath.ToSlash(file)
		name := strings.ToLower(path.Base(file))
		index.byName[name] = append(index.byName[name], file)
	}
	return index
}

// BuildVaultFileIndex walks the vault root the way Obsidian loads a vault.
// ponytail: skips only dot-folders and node_modules; a repository vault with
// huge vendored trees pays one walk per run, so callers build it lazily.
func BuildVaultFileIndex(vaultDef VaultDefinition) (*VaultFileIndex, error) {
	root := vaultFileRoot(vaultDef)
	if root == "" {
		return NewVaultFileIndex(nil), nil
	}
	var files []string
	err := filepath.WalkDir(root, func(abs string, entry fs.DirEntry, err error) error {
		if err != nil {
			if abs == root {
				return err
			}
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if abs != root && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return NewVaultFileIndex(files), nil
}

// vaultFileRoot is the directory note paths are relative to.
func vaultFileRoot(vaultDef VaultDefinition) string {
	if vaultDef.IsCollection() {
		return vaultDef.Root
	}
	return vaultDef.Path
}

// Resolve returns the file Obsidian opens for linkpath (fragment already
// removed) authored in sourcePath.
func (x *VaultFileIndex) Resolve(linkpath, sourcePath string) (string, bool) {
	if x == nil || strings.TrimSpace(linkpath) == "" {
		return "", false
	}
	lower := strings.ToLower(filepath.ToSlash(linkpath))
	candidates := x.byName[path.Base(lower)]
	if len(candidates) == 0 || !strings.Contains(path.Base(lower), ".") {
		lower += ".md"
		candidates = x.byName[path.Base(lower)]
	}
	if len(candidates) == 0 {
		return "", false
	}
	if !strings.Contains(lower, "/") && len(candidates) == 1 {
		return candidates[0], true
	}
	sourceDir := strings.ToLower(filepath.ToSlash(path.Dir(filepath.ToSlash(sourcePath))))
	if sourceDir == "." {
		sourceDir = ""
	}
	if strings.HasPrefix(lower, "./") || strings.HasPrefix(lower, "../") {
		joined := path.Clean(path.Join(sourceDir, lower))
		for _, candidate := range candidates {
			if strings.ToLower(candidate) == joined {
				return candidate, true
			}
		}
	}
	exact := strings.TrimPrefix(lower, "/")
	for _, candidate := range candidates {
		if strings.ToLower(candidate) == exact {
			return candidate, true
		}
	}
	if strings.HasPrefix(linkpath, "/") {
		return "", false
	}
	var near, far []string
	for _, candidate := range candidates {
		folded := strings.ToLower(candidate)
		if !strings.HasSuffix(folded, lower) {
			continue
		}
		if strings.HasPrefix(folded, sourceDir) {
			near = append(near, candidate)
		} else {
			far = append(far, candidate)
		}
	}
	ordered := append(sortByPathLength(near), sortByPathLength(far)...)
	if len(ordered) == 0 {
		return "", false
	}
	return ordered[0], true
}

func sortByPathLength(paths []string) []string {
	sort.SliceStable(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) < len(paths[j])
		}
		return paths[i] < paths[j]
	})
	return paths
}

// Ambiguous reports whether a folderless linkpath names several files, so the
// file it opens depends on Obsidian's tie-break rather than the name alone.
func (x *VaultFileIndex) Ambiguous(linkpath string) bool {
	if x == nil {
		return false
	}
	lower := strings.ToLower(filepath.ToSlash(linkpath))
	if strings.Contains(lower, "/") {
		return false
	}
	candidates := x.byName[lower]
	if len(candidates) == 0 || !strings.Contains(lower, ".") {
		candidates = x.byName[lower+".md"]
	}
	return len(candidates) > 1
}
