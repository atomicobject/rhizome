//go:build darwin

package watchhub

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
)

var fseventsRetryDelays = []time.Duration{
	100 * time.Millisecond,
	300 * time.Millisecond,
	1 * time.Second,
}

const fsnotifyFallbackHeadroom = 64

func (h *Hub) maybeFallbackBackend(reason error) bool {
	backend := h.backendSnapshot()
	if backend == nil {
		return false
	}
	if _, ok := backend.(*fsnotifyBackend); ok {
		return false
	}
	fseventsBackend, ok := backend.(*fseventsBackend)
	if !ok {
		return false
	}
	if h.context() == nil {
		return false
	}

	if h.maybeRetryFSEvents(fseventsBackend, reason) {
		return true
	}

	return h.fallbackToFSNotify(fseventsBackend, reason)
}

func (h *Hub) fallbackToFSNotify(prev Backend, reason error) bool {
	if ok, msg := h.fsnotifyFallbackAllowed(); !ok {
		h.logger.Printf("watchhub: skipping fsnotify fallback (%s): %v", msg, reason)
		return false
	}
	fallback, err := newFSNotifyBackend(h.eventBuffer)
	if err != nil {
		h.logger.Printf("watchhub: fsnotify fallback failed: %v", err)
		return false
	}
	if err := fallback.Start(h.context()); err != nil {
		h.logger.Printf("watchhub: fsnotify fallback start failed: %v", err)
		return false
	}

	h.roots.ResetWatches()
	h.roots.ResetWalked()
	h.setBackend(fallback)
	h.AddWatchRoots()
	h.logger.Printf("watchhub: falling back to fsnotify after fsevents error: %v", reason)

	if prev != nil {
		_ = prev.Close()
	}
	h.fseventsMu.Lock()
	h.fseventsTry = 0
	h.fseventsWait = false
	h.fseventsMu.Unlock()
	return true
}

func (h *Hub) maybeRetryFSEvents(backend *fseventsBackend, reason error) bool {
	h.fseventsMu.Lock()
	if h.fseventsWait {
		h.fseventsMu.Unlock()
		return true
	}
	if h.fseventsTry >= len(fseventsRetryDelays) {
		h.fseventsMu.Unlock()
		return false
	}
	delay := fseventsRetryDelays[h.fseventsTry]
	h.fseventsTry++
	h.fseventsWait = true
	h.fseventsMu.Unlock()

	h.logger.Printf("watchhub: fsevents start failed, retrying in %s: %v", delay, reason)

	if !h.beginWork() {
		return true
	}
	go func() {
		defer h.work.Done()
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-h.pendingStop:
			return
		case <-timer.C:
		}
		h.fseventsMu.Lock()
		h.fseventsWait = false
		h.fseventsMu.Unlock()

		if h.context() == nil || h.context().Err() != nil {
			return
		}

		current, ok := h.backendSnapshot().(*fseventsBackend)
		if !ok || current != backend {
			return
		}
		if err := backend.startStream(); err != nil {
			h.logger.Printf("watchhub: fsevents retry failed: %v", err)
			if !h.maybeRetryFSEvents(backend, err) {
				_ = h.fallbackToFSNotify(backend, err)
			}
			return
		}
		h.fseventsMu.Lock()
		h.fseventsTry = 0
		h.fseventsMu.Unlock()
	}()

	return true
}

func (h *Hub) fsnotifyFallbackAllowed() (bool, string) {
	soft, hard, err := getNoFileLimit()
	if err != nil {
		return true, ""
	}
	if soft == 0 || soft == syscall.RLIM_INFINITY {
		return true, ""
	}
	watches := h.estimateFSNotifyWatches()
	if watches == 0 {
		return true, ""
	}
	needed := uint64(watches) + fsnotifyFallbackHeadroom
	if soft <= needed {
		return false, fmt.Sprintf("soft limit %d < watches %d + headroom %d (hard %d)", soft, watches, fsnotifyFallbackHeadroom, hard)
	}
	return true, ""
}

func getNoFileLimit() (uint64, uint64, error) {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		return 0, 0, err
	}
	return lim.Cur, lim.Max, nil
}

func (h *Hub) estimateFSNotifyWatches() int {
	roots := h.roots.List()
	if len(roots) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(roots))
	total := 0
	add := func(path string) {
		normalized := paths.NormalizeAbsPathForCompare(path)
		if normalized == "" {
			return
		}
		key := paths.CaseKey(normalized)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		total++
	}
	for _, root := range roots {
		if root.Path == "" {
			continue
		}
		if root.Options.Recursive {
			add(root.Path)
			continue
		}
		info, err := os.Stat(root.Path)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root.Path, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if !root.Options.IncludeHidden && isHidden(path) {
				if path != root.Path {
					return filepath.SkipDir
				}
			}
			rel, relErr := h.vault.RelStrict(path)
			if relErr == nil && rel.String() != "" {
				if h.shouldFilter(WatchEvent{RelPath: paths.NormalizeRelPathAuto(rel.String())}, true) {
					if path != root.Path {
						return filepath.SkipDir
					}
				}
			}
			add(path)
			return nil
		})
	}
	return total
}
