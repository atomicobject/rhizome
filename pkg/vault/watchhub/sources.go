package watchhub

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/paths"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (h *Hub) EmitHintPaths(paths []string) {
	hints := make([]HintPath, 0, len(paths))
	for _, rel := range paths {
		hints = append(hints, HintPath{Path: rel, Kind: "modified"})
	}
	h.EmitHintPathHints(hints)
}

// HintPath describes a vault-relative leader/follower path hint.
type HintPath struct {
	Path string
	Kind string
}

func (h *Hub) EmitHintPathHints(hints []HintPath) {
	for _, hint := range hints {
		rel := strings.TrimSpace(hint.Path)
		if rel == "" {
			continue
		}
		rel = filepath.ToSlash(rel)
		rel = strings.TrimPrefix(rel, "./")
		if rel == "." {
			continue
		}
		if strings.HasPrefix(rel, rhizomeDirName+"/") || rel == rhizomeDirName || shouldDropVaultRelPath(rel) {
			continue
		}
		op := hintPathOp(hint.Kind)
		if op == 0 {
			continue
		}
		// Hint records are already vault-relative. Reconstruct AbsPath only for
		// subscribers that need to stat/read; filtering and dedupe stay on RelPath.
		ev := WatchEvent{
			RelPath: rel,
			AbsPath: filepath.Join(h.vaultRoot, filepath.FromSlash(rel)),
			Op:      op,
			IsDir:   false,
			Root:    h.vaultRoot,
			Source:  SourceHint,
		}
		if h.shouldFilter(ev, false) {
			continue
		}
		h.enqueue(ev)
	}
}

func hintPathOp(kind string) Op {
	switch strings.TrimSpace(kind) {
	case "created":
		return OpCreate
	case "removed":
		return OpRemove
	case "renamed":
		return OpRename
	case "recreated":
		return OpRemove | OpCreate
	case "modified", "unknown", "":
		return OpWrite
	default:
		return OpWrite
	}
}

func (h *Hub) EmitHintResync() {
	h.reloadIgnore(nil)
	h.markStale(StaleEvent{Reason: StaleHintResync, Source: SourceHint})
}

func (h *Hub) fsLoop(ctx context.Context) {
	if h.debug {
		h.logger.Printf("watchhub: fsLoop started, waiting for events")
	}
	for {
		backend := h.backendSnapshot()
		if backend == nil {
			select {
			case <-ctx.Done():
				return
			case <-h.pendingStop:
				return
			case <-h.backendCh:
				continue
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}
		select {
		case <-ctx.Done():
			if h.debug {
				h.logger.Printf("watchhub: fsLoop stopping (context done)")
			}
			return
		case <-h.pendingStop:
			if h.debug {
				h.logger.Printf("watchhub: fsLoop stopping (pending stop)")
			}
			return
		case <-h.backendCh:
			continue
		case ev, ok := <-backend.Events():
			if !ok {
				if h.backendSnapshot() != backend {
					continue
				}
				if h.debug {
					h.logger.Printf("watchhub: backend events channel closed")
				}
				h.markStale(StaleEvent{Reason: StaleWatcherError, Source: SourceFSNotify})
				return
			}
			h.handleBackendEvent(ev)
		case err, ok := <-backend.Errors():
			if !ok {
				if h.backendSnapshot() != backend {
					continue
				}
				h.markStale(StaleEvent{Reason: StaleWatcherError, Source: SourceFSNotify})
				return
			}
			if h.backendSnapshot() != backend {
				continue
			}
			h.logger.Printf("watchhub: watcher error: %v", err)
			h.markStale(StaleEvent{Reason: StaleWatcherError, Source: SourceFSNotify})
		}
	}
}

func (h *Hub) handleBackendEvent(ev BackendEvent) {
	if h.debug {
		h.logger.Printf("watchhub: backend event: %s op=%v", ev.Path, ev.Op)
	}

	// Handle MustRescan (FSEvents coalescing) - trigger full stale unless the
	// rescanned tree holds nothing the hub would deliver.
	if ev.MustRescan {
		if !h.rescanMayCoverWatchedPaths(ev.Path) {
			if h.debug {
				h.logger.Printf("watchhub: dropping rescan of ignored tree: %s", ev.Path)
			}
			return
		}
		h.markStale(StaleEvent{
			Reason: StaleOverflow,
			Source: SourceFSNotify,
			Path:   ev.Path,
		})
		return
	}

	abs := filepath.Clean(ev.Path)
	if abs == "" {
		if h.debug {
			h.logger.Printf("watchhub: dropping event with empty path")
		}
		return
	}
	if shouldDropBackendPath(abs) {
		if h.debug {
			h.logger.Printf("watchhub: dropping internal path: %s", abs)
		}
		return
	}
	// Hard drops come first so a .gitignore inside .git or node_modules does
	// not trigger an ignore reload and full resync.
	rel, relErr := h.vault.RelStrict(abs)
	if relErr == nil && shouldDropVaultRelPath(rel.String()) {
		if h.debug {
			h.logger.Printf("watchhub: dropping ignored vault path: %s", rel.String())
		}
		return
	}
	if isIgnoreFile(abs) {
		h.reloadIgnore(nil)
		h.roots.ResetWalked()
		h.AddWatchRoots()
		h.markStale(StaleEvent{Reason: StaleIgnoreChanged, Source: SourceFSNotify, Path: abs})
		return
	}

	if relErr != nil || rel.String() == "" {
		if h.debug {
			h.logger.Printf("watchhub: dropping event outside vault: %s (err=%v)", abs, relErr)
		}
		return
	}
	relPath := paths.NormalizeRelPathAuto(rel.String())
	root := h.roots.MatchRoot(abs)

	// Use IsDir from backend if known, otherwise stat
	isDir := ev.IsDir
	if !ev.IsDirKnown {
		if info, err := os.Stat(abs); err == nil {
			isDir = info.IsDir()
		}
	}

	wev := WatchEvent{
		RelPath: relPath,
		AbsPath: abs,
		Op:      ev.Op,
		IsDir:   isDir,
		Root:    root,
		Source:  SourceFSNotify,
	}

	if h.shouldFilter(wev, isDir) {
		if h.debug {
			h.logger.Printf("watchhub: dropping ignored path: %s", relPath)
		}
		return
	}

	if isDir && !h.roots.IsRecursiveBackend() && wev.Op.Has(OpCreate) {
		h.beginPendingAdd()
		select {
		case h.pendingAdd <- abs:
		default:
			h.finishPendingAdd()
			h.markStale(StaleEvent{Reason: StaleOverflow, Source: SourceFSNotify, Path: abs})
		}
	}

	if wev.Op.Has(OpRemove) || wev.Op.Has(OpRename) {
		wasDir := h.removeWatch(abs)
		if wasDir && !wev.IsDir {
			wev.IsDir = true
		}
		if wasDir && wev.Op.Has(OpRename) {
			h.markStale(StaleEvent{Reason: StaleDirRenamed, Source: SourceFSNotify, Path: abs})
		}
	}

	if h.debug {
		h.logger.Printf("watchhub: file event: %s op=%v", relPath, wev.Op)
	}
	h.enqueue(wev)
}

// rescanMayCoverWatchedPaths reports whether a MustScanSubDirs rescan of dir
// could have lost events the hub would deliver. It answers false only when
// every path under dir is dropped or ignored; the vault root, paths outside
// the vault, and ancestors of watched roots stay conservative.
func (h *Hub) rescanMayCoverWatchedPaths(dir string) bool {
	abs := filepath.Clean(dir)
	if shouldDropBackendPath(abs) {
		return false
	}
	rel, err := h.vault.RelStrict(abs)
	if err != nil || rel.String() == "" || rel.String() == "." {
		return true
	}
	cmp := paths.NormalizeAbsPathForCompare(abs)
	for _, root := range h.roots.List() {
		if root.Path == cmp || strings.HasPrefix(root.Path, cmp+"/") {
			return true
		}
	}
	relPath := paths.NormalizeRelPathAuto(rel.String())
	// .rhizome is dropped except for config, ignore rules, and ontology
	// schemas, so only the directories that can hold those stay watched.
	lower := strings.ToLower(relPath)
	if lower == rhizomeDirName || lower == rhizomeDirName+"/ontology" || strings.HasPrefix(lower, rhizomeDirName+"/ontology/") {
		return true
	}
	if shouldDropVaultRelPath(relPath) {
		return false
	}
	// An ignored directory hides every descendant, because the matcher
	// checks ancestors.
	return !h.shouldFilter(WatchEvent{RelPath: relPath, IsDir: true}, true)
}
