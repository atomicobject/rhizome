package watchhub

import (
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"path/filepath"
	"strings"
	"time"
)

type Filter struct {
	Prefixes      []string
	IncludeDirs   bool
	IncludeFiles  bool
	IncludeHidden bool
}

func (f Filter) Matches(ev WatchEvent) bool {
	if ev.RelPath == "" {
		return false
	}
	if !f.IncludeHidden && isHidden(ev.RelPath) {
		return false
	}
	if !f.IncludeDirs && ev.IsDir {
		return false
	}
	if !f.IncludeFiles && !ev.IsDir {
		return false
	}
	if len(f.Prefixes) > 0 {
		matched := false
		for _, prefix := range f.Prefixes {
			if prefix == "" {
				continue
			}
			if ev.RelPath == prefix || strings.HasPrefix(ev.RelPath, prefix+"/") {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

func (h *Hub) markStale(ev StaleEvent) {
	if !h.beginWork() {
		return
	}
	defer h.work.Done()
	if h.shouldSuppressStale(ev) {
		if h.debug {
			h.logger.Printf("watchhub: stale coalesced reason=%s source=%s path=%s", ev.Reason, ev.Source, ev.Path)
		}
		return
	}
	h.subsMu.RLock()
	subs := make([]*subscription, 0, len(h.subs))
	for _, sub := range h.subs {
		subs = append(subs, sub)
	}
	h.subsMu.RUnlock()

	for _, sub := range subs {
		if sub == nil || sub.onStale == nil || !sub.active.Load() {
			continue
		}
		if sub.active.Load() {
			sub.onStale(h.context(), ev)
		}
	}
}

func (h *Hub) shouldSuppressStale(ev StaleEvent) bool {
	cooldown := h.staleCooldownFor(ev.Reason)
	if cooldown <= 0 {
		return false
	}
	now := h.now()
	h.staleMu.Lock()
	defer h.staleMu.Unlock()
	if last, ok := h.staleLast[ev.Reason]; ok && now.Sub(last) < cooldown {
		return true
	}
	h.staleLast[ev.Reason] = now
	return false
}

func (h *Hub) staleCooldownFor(reason StaleReason) time.Duration {
	switch reason {
	case StaleIgnoreChanged, StaleDirRenamed, StaleDirCreated:
		return 0
	default:
		return h.staleMin
	}
}

func (h *Hub) reloadIgnore(userExcludes []string) {
	h.ignoreMu.Lock()
	defer h.ignoreMu.Unlock()
	if userExcludes != nil {
		h.userExcludes = append([]string(nil), userExcludes...)
	}
	h.ignore = ignore.LoadUnifiedMatcher(h.vaultRoot, h.userExcludes)
	h.hardIgnore = ignore.LoadUnifiedMatcher(h.vaultRoot, nil)
}

func (h *Hub) shouldFilter(ev WatchEvent, isDir bool) bool {
	h.ignoreMu.RLock()
	matcher := h.ignore
	hardMatcher := h.hardIgnore
	h.ignoreMu.RUnlock()
	if !isDir && ignore.IsSystemContextPath(ev.RelPath) {
		return ignore.IsDefaultInfrastructurePath(ev.RelPath) || (hardMatcher != nil && hardMatcher.IsIgnored(ev.RelPath, false))
	}
	if matcher == nil || !matcher.IsIgnored(ev.RelPath, isDir) {
		return false
	}
	return !isDir || hardMatcher == nil || hardMatcher.IsIgnored(ev.RelPath, true)
}

func isHidden(path string) bool {
	base := filepath.Base(path)
	return strings.HasPrefix(base, ".")
}

func isIgnoreFile(path string) bool {
	base := filepath.Base(path)
	dir := filepath.Base(filepath.Dir(path))
	if dir == rhizomeDirName && base == "ignore" {
		return true
	}
	return base == ".obsidianignore" || base == ".gitignore"
}

func shouldWatchInternalRoot(path string) bool {
	return filepath.Base(filepath.Clean(path)) == rhizomeDirName
}
