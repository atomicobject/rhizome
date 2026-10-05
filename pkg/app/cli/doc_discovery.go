package actions

import (
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type docMatch struct {
	AbsPath string
	Path    string
	Dir     string
	Pattern string
}

func collectAncestorDocMatches(startDir string, rootPaths paths.VaultPaths, patterns []string, maxEmpty int) []docMatch {
	if startDir == "" {
		return nil
	}
	patterns = obsidian.NormalizeDocPatterns(patterns)
	if len(patterns) == 0 {
		return nil
	}
	root := rootPaths.Root()
	if root == "" {
		root = startDir
	}

	var docs []docMatch
	emptyStreak := 0
	for dir := startDir; ; {
		doc, ok := findDocMatchInDir(dir, patterns, rootPaths)
		if ok {
			emptyStreak = 0
			docs = append(docs, doc)
		} else {
			emptyStreak++
			if maxEmpty > 0 && emptyStreak >= maxEmpty {
				break
			}
		}
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !isSubdir(parent, rootPaths) {
			break
		}
		dir = parent
	}
	return docs
}

func findDocMatchInDir(dir string, patterns []string, rootPaths paths.VaultPaths) (docMatch, bool) {
	patterns = obsidian.NormalizeDocPatterns(patterns)
	for _, pattern := range patterns {
		candidate := filepath.Join(dir, pattern)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		return buildDocMatch(dir, candidate, pattern, rootPaths), true
	}
	return docMatch{}, false
}

func buildDocMatch(dir, path, pattern string, rootPaths paths.VaultPaths) docMatch {
	relDir := filepath.ToSlash(dir)
	relPath := filepath.ToSlash(path)
	if rel, err := rootPaths.RelStrict(dir); err == nil && rel.String() != "" {
		relDir = rel.String()
	}
	if rel, err := rootPaths.RelStrict(path); err == nil && rel.String() != "" {
		relPath = rel.String()
	}
	return docMatch{
		AbsPath: path,
		Dir:     relDir,
		Path:    relPath,
		Pattern: pattern,
	}
}
