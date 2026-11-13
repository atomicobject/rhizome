package bootstrap

import (
	"fmt"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
)

// The caller holds ownershipConfigMu so startup cannot reinstall removed roots.
func (rt *LiveRuntime) updateCodeWatchRoots(previous, next codeanchor.Config) error {
	hub := rt.hub.Load()
	if hub == nil {
		return nil
	}
	roots := make(map[string]struct{})
	if next.Enabled {
		for _, root := range liveCodeRoots(rt.VaultPath, next) {
			path := paths.ResolveSymlinks(root.String()).String()
			roots[path] = struct{}{}
			if err := hub.AddRoot(path, watchhub.RootOptions{Kind: watchhub.RootCode}); err != nil {
				return fmt.Errorf("register code watch root: %w", err)
			}
		}
	}
	if previous.Enabled {
		for _, root := range liveCodeRoots(rt.VaultPath, previous) {
			path := paths.ResolveSymlinks(root.String()).String()
			if _, keep := roots[path]; !keep && paths.NormalizeAbsPathForCompare(path) != paths.NormalizeAbsPathForCompare(rt.VaultPath) {
				hub.RemoveRoot(path)
			}
		}
	}
	return nil
}
