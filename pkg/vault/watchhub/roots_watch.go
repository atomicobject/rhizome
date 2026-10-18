package watchhub

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/paths"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (h *Hub) addWatch(path string) {
	backend := h.backendSnapshot()
	if backend == nil {
		return
	}
	if path == "" {
		return
	}
	clean := filepath.Clean(path)
	if !h.roots.TrackWatch(clean) {
		return
	}
	if err := backend.AddPath(clean); err != nil {
		if h.maybeFallbackBackend(err) {
			return
		}
		h.roots.UntrackWatch(clean)
		h.markStale(StaleEvent{Reason: StaleWatcherError, Source: SourceFSNotify, Path: clean})
		h.logger.Printf("watchhub: add watch %s failed: %v", clean, err)
		return
	}
	h.noteReadyAfter(backend)
	if h.debug {
		h.logger.Printf("watchhub: added watch: %s", clean)
	}
}

func (h *Hub) removeWatch(path string) bool {
	backend := h.backendSnapshot()
	if backend == nil {
		return false
	}
	clean := filepath.Clean(path)
	removed := h.roots.UntrackWatchTree(clean)
	if len(removed) == 0 {
		return false
	}
	for _, watched := range removed {
		_ = backend.RemovePath(watched)
	}
	h.noteReadyAfter(backend)
	return true
}

func (h *Hub) AddWatchRoots() {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	h.beginWatchInstall()
	defer h.finishWatchInstall()

	roots := h.roots.List()
	for _, root := range roots {
		if h.isStopping() {
			return
		}
		if root.Path == "" {
			continue
		}
		if root.Options.Kind == RootInternal && !shouldWatchInternalRoot(root.Path) {
			continue
		}
		if h.roots.IsRecursiveBackend() || root.Options.Recursive {
			h.addWatch(root.Path)
			continue
		}
		if h.roots.HasWalked(root.Path) {
			continue
		}
		if err := h.addRecursive(root.Path, root.Options); err != nil {
			h.logger.Printf("watchhub: add root %s failed: %v", root.Path, err)
			continue
		}
		// Only mark as walked if we actually have a backend to add watches to.
		// Otherwise we'd skip re-walking when EnableFSNotify() creates the backend later.
		if h.backendSnapshot() != nil {
			h.roots.MarkWalked(root.Path)
		}
	}
	// Log total watch count for diagnostics
	if backend := h.backendSnapshot(); backend != nil && h.debug {
		h.logger.Printf("watchhub: watching %d paths", len(backend.WatchList()))
	}
}

func (h *Hub) addRecursiveInstall(root string, opts RootOptions) error {
	h.beginWatchInstall()
	defer h.finishWatchInstall()
	return h.addRecursive(root, opts)
}

func (h *Hub) addRecursive(root string, opts RootOptions) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	if h.debug {
		h.logger.Printf("watchhub: walking directory tree from: %s", root)
	}
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if h.isStopping() {
			return context.Canceled
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			entry := h.roots.matchRoot(path)
			if entry.Path == "" {
				entry = RootEntry{Path: paths.NormalizeAbsPathForCompare(root), Options: opts}
			}
			if !entry.includesDirectory(path) {
				if path != root {
					if h.debug {
						h.logger.Printf("watchhub: skipping hidden dir: %s", path)
					}
					return filepath.SkipDir
				}
			}
			rel, relErr := h.vault.RelStrict(path)
			if relErr == nil && rel.String() != "" {
				if h.shouldFilter(WatchEvent{RelPath: paths.NormalizeRelPathAuto(rel.String())}, true) {
					if path != root {
						if h.debug {
							h.logger.Printf("watchhub: skipping ignored dir: %s", path)
						}
						return filepath.SkipDir
					}
				}
			}
			h.addWatch(path)
		}
		return nil
	})
}

func (h *Hub) isStopping() bool {
	if h == nil {
		return true
	}
	if ctx := h.context(); ctx != nil {
		select {
		case <-ctx.Done():
			return true
		default:
		}
	}
	select {
	case <-h.pendingStop:
		return true
	default:
		return false
	}
}

func (h *Hub) addIgnoreRoot(vaultRoot string) {
	if strings.TrimSpace(vaultRoot) == "" {
		return
	}
	internalPath := filepath.Join(vaultRoot, rhizomeDirName)
	if info, err := os.Stat(internalPath); err != nil || !info.IsDir() {
		return
	}
	internalOpts := RootOptions{
		Kind:          RootInternal,
		IncludeHidden: true,
	}
	_ = h.roots.Add(internalPath, internalOpts)
	if h.backendSnapshot() == nil {
		return
	}
	if h.roots.IsRecursiveBackend() {
		h.addWatch(internalPath)
		return
	}
	if h.roots.HasWalked(internalPath) {
		return
	}
	if err := h.addRecursiveInstall(internalPath, internalOpts); err == nil {
		h.roots.MarkWalked(internalPath)
	}
}

func (h *Hub) beginWatchInstall() {
	h.readyMu.Lock()
	h.watchInstall++
	h.readyMu.Unlock()
}

func (h *Hub) finishWatchInstall() {
	backend := h.backendSnapshot()
	h.readyMu.Lock()
	if h.watchInstall > 0 {
		h.watchInstall--
	}
	h.bumpReadyAfterLocked(backend)
	h.readyMu.Unlock()
}

func (h *Hub) beginPendingAdd() {
	h.readyMu.Lock()
	h.pendingWork++
	h.readyMu.Unlock()
}

func (h *Hub) finishPendingAdd() {
	backend := h.backendSnapshot()
	h.readyMu.Lock()
	if h.pendingWork > 0 {
		h.pendingWork--
	}
	h.bumpReadyAfterLocked(backend)
	h.readyMu.Unlock()
}

func (h *Hub) noteReadyAfter(backend Backend) {
	h.readyMu.Lock()
	h.bumpReadyAfterLocked(backend)
	h.readyMu.Unlock()
}

func (h *Hub) readinessSnapshot() (watchInstall int, pendingWork int, pendingQueued int, readyAfter time.Time) {
	h.readyMu.RLock()
	defer h.readyMu.RUnlock()
	return h.watchInstall, h.pendingWork, len(h.pendingAdd), h.readyAfter
}

func (h *Hub) bumpReadyAfterLocked(backend Backend) {
	readyAt := h.now().Add(backendReadySettle(backend))
	if readyAt.After(h.readyAfter) {
		h.readyAfter = readyAt
	}
}

func backendReadySettle(backend Backend) time.Duration {
	if backendIsFSEvents(backend) {
		return time.Second
	}
	if _, ok := backend.(*fsnotifyBackend); ok {
		return 50 * time.Millisecond
	}
	return 0
}
