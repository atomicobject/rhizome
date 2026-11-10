package watchhub

import (
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
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

// dropsAtIngress applies the hub's hard drops to a backend's absolute path, so
// generated .rhizome state such as the index database never occupies the
// backend's bounded buffer. Backends call it from their own goroutine.
func (h *Hub) dropsAtIngress(abs string) bool {
	if shouldDropBackendPath(abs) {
		return true
	}
	rel, err := h.vault.RelStrict(filepath.Clean(abs))
	return err == nil && shouldDropVaultRelPath(rel.String())
}

func shouldDropVaultRelPath(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	rel := filepath.ToSlash(filepath.Clean(path))
	p := strings.ToLower(rel)
	// Git metadata is never vault content, even when a .rhizome/ignore file
	// replaces the default ignores; a periodic fetch would otherwise refresh
	// every open view.
	if hasPathSegment(p, ".git") || hasPathSegment(p, ".claude") || hasPathSegment(p, "node_modules") {
		return true
	}
	if !hasPathSegment(p, ".rhizome") {
		return false
	}
	// Most .rhizome/ files are generated cache/index state and would cause
	// self-triggering watcher churn. Config, ignore rules, and ontology schemas
	// (flat or in a module) are the intentional exceptions because they change
	// how the vault is read.
	if p == ".rhizome/config.yml" || p == ".rhizome/ignore" {
		return false
	}
	if assetroots.IsAssetPath(rel, assetroots.Ontology) && strings.HasSuffix(rel, ".graphql") {
		return false
	}
	// Ontology folders can appear after startup; non-recursive backends
	// only watch them if their create events get through.
	return !assetroots.IsAssetRootOrAncestor(rel, assetroots.Ontology)
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
