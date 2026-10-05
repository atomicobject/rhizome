package watchhub

import (
	"context"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

func (h *Hub) pendingAddLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.pendingStop:
			return
		case path := <-h.pendingAdd:
			h.installPendingDirectory(path)
			h.finishPendingAdd()
		}
	}
}

func (h *Hub) installPendingDirectory(path string) {
	if h.isStopping() {
		return
	}
	installPaths := []string{path}
	normalized := paths.NormalizeAbsPathForCompare(path)
	for _, root := range h.roots.List() {
		if strings.HasPrefix(root.Path, normalized+"/") {
			installPaths = append(installPaths, root.Path)
		}
	}
	installed := false
	for _, candidate := range installPaths {
		root := h.roots.matchRoot(candidate)
		if root.Path == "" || !root.includesDirectory(candidate) {
			continue
		}
		rel, err := h.vault.RelStrict(candidate)
		if err != nil || h.shouldFilter(WatchEvent{RelPath: paths.NormalizeRelPathAuto(rel.String())}, true) {
			continue
		}
		if err := h.addRecursiveInstall(candidate, root.Options); err != nil {
			return
		}
		installed = true
	}
	if installed && !h.isStopping() {
		h.markStale(StaleEvent{Reason: StaleDirCreated, Source: SourceFSNotify, Path: path})
	}
}
