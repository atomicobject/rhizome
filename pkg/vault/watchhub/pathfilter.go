package watchhub

import (
	"path/filepath"
	"strings"
)

// isNoisyInternalPath filters high-churn local build/cache paths that can flood
// watcher buffers without contributing useful index updates.
func isNoisyInternalPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	p := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if strings.Contains(p, "/.gocache/") || strings.HasSuffix(p, "/.gocache") {
		return true
	}
	if strings.Contains(p, "/.gotmp/") || strings.HasSuffix(p, "/.gotmp") {
		return true
	}
	// Worktree-local Go build cache can generate extreme event volume.
	if strings.Contains(p, "/worktrees/") && strings.Contains(p, "/.gocache/") {
		return true
	}
	return false
}

func shouldDropBackendPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	p := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if isNoisyInternalPath(p) {
		return true
	}
	return false
}

func shouldDropVaultRelPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	p := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if hasPathSegment(p, ".claude") || hasPathSegment(p, "node_modules") {
		return true
	}
	if !hasPathSegment(p, ".rhizome") {
		return false
	}
	// Most .rhizome/ files are generated cache/index state and would cause
	// self-triggering watcher churn. Config, ignore rules, and ontology schemas
	// are the intentional exceptions because they change how the vault is read.
	if p == ".rhizome/config.yml" || p == ".rhizome/ignore" {
		return false
	}
	if strings.HasPrefix(p, ".rhizome/ontology/") && strings.HasSuffix(p, ".graphql") {
		return false
	}
	return true
}

func hasPathSegment(path, segment string) bool {
	path = strings.Trim(path, "/")
	if path == "" || segment == "" {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == segment {
			return true
		}
	}
	return false
}
