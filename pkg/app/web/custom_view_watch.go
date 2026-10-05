package web

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// GlobalEventViewsChanged announces that a file in a repository view folder
// changed. Its data is {folder}, the definition folder relative to
// .rhizome/views ("." for the root), matching the folder in the view shell's
// config. Bundled views change only with the binary and are never announced.
const GlobalEventViewsChanged = "views.changed"

// WHY: the vault watcher drops .rhizome/ paths by design, and letting view
// sources through would feed them to indexing and validation reconciliation.
// One poller per server, alive only while an event stream is open, replaces
// the one-second stamp poll each hosted frame used to run.
// ponytail: polling stats every view file each second; move to a watch-hub
// subscription if view folders grow large enough for that walk to matter.
type viewFolderWatch struct {
	mu        sync.Mutex
	listeners int
	// stop cancels the poller and returns once it has exited.
	stop func()
}

const viewFolderPollInterval = time.Second

// watchViewFolders keeps the poller running until the returned release is
// called by every listener. Once the last release returns, nothing more is
// announced.
func (s *Server) watchViewFolders() (release func()) {
	w := &s.viewWatch
	w.mu.Lock()
	defer w.mu.Unlock()
	w.listeners++
	if w.listeners == 1 {
		ctx, cancel := context.WithCancel(context.Background())
		done := s.pollViewFolders(ctx, viewFolderPollInterval)
		w.stop = func() {
			cancel()
			<-done
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			w.listeners--
			if w.listeners == 0 {
				w.stop()
				w.stop = nil
			}
		})
	}
}

// pollViewFolders takes its baseline before returning, so a change made after
// a listener connects is always announced, then polls until ctx ends. The
// returned channel closes when polling has stopped.
func (s *Server) pollViewFolders(ctx context.Context, interval time.Duration) <-chan struct{} {
	whole, folders := s.viewFolderStamps("")
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.announceViewFolderChanges(ctx, interval, whole, folders)
	}()
	return done
}

func (s *Server) announceViewFolderChanges(ctx context.Context, interval time.Duration, whole string, folders map[string]string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		var next map[string]string
		whole, next = s.viewFolderStamps(whole)
		if next == nil {
			continue
		}
		for folder, stamp := range next {
			if folders[folder] != stamp {
				s.NotifyGlobalEvent(GlobalEventViewsChanged, map[string]string{"folder": folder})
			}
		}
		for folder := range folders {
			if _, ok := next[folder]; !ok {
				s.NotifyGlobalEvent(GlobalEventViewsChanged, map[string]string{"folder": folder})
			}
		}
		folders = next
	}
}

// viewFolderStamps returns a stamp of the whole views folder and, only when it
// differs from previous, the stamp of every custom view's definition folder.
// Definitions are read only after something changed.
func (s *Server) viewFolderStamps(previous string) (string, map[string]string) {
	root, err := os.OpenRoot(s.customViewsRoot())
	if err != nil {
		if previous == "missing" {
			return previous, nil
		}
		return "missing", map[string]string{}
	}
	defer root.Close()
	whole := customViewFolderStamp(root.FS(), ".")
	if whole == previous {
		return whole, nil
	}
	defs, _ := viewconfig.LoadDefaultSource(s.cfg.VaultPath)
	folders := map[string]string{}
	for _, def := range defs {
		if def.SourceSpec.Kind != viewconfig.SourceKindCustom {
			continue
		}
		rel, err := filepath.Rel(s.customViewsRoot(), filepath.Dir(def.Source.Path))
		if err != nil {
			continue
		}
		folder := filepath.ToSlash(rel)
		if _, done := folders[folder]; !done {
			folders[folder] = customViewFolderStamp(root.FS(), folder)
		}
	}
	return whole, folders
}
