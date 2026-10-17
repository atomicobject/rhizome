package web

import (
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	codeemb "github.com/atomicobject/rhizome/pkg/embeddings/codeindex"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/watchhub"
	"sync"
)

// Runtime holds dependencies for API handlers.
//
// The index-readiness fields implement the one-shot gate used by
// requireIndexReady to defer index-dependent reads during first launch and
// post-migration windows. See gate.go for the gate contract.
type Runtime struct {
	configuration *repoconfig.Resolved
	mu            sync.RWMutex
	Live          runtimeview.View
	NoteIndex     embeddings.Index
	NoteProvider  embeddings.Provider
	CodeIndex     codeemb.Index
	CodeProvider  embeddings.Provider
	IntelStore    *semdb.Store
	Ignore        *ignore.Matcher
	WatchHub      *watchhub.Hub
	ownsNote      bool
	ownsCode      bool
	ownsIntel     bool
	hubListeners  []func(*watchhub.Hub)
	liveHealth    interface {
		LiveHealth() bootstrap.LiveHealth
	}
	indexReady                 chan struct{}
	indexReadyOnce             sync.Once
	noteReadReady              chan struct{}
	noteReadReadyOnce          sync.Once
	indexGated                 bool
	validationRepairAuthority  ValidationRepairAuthority
	repairPathCoordinator      *validate.RepairPathCoordinator
	validationRefreshRequester ValidationRefreshRequester
}

func (r *Runtime) NoteEmbeddings() (embeddings.Index, embeddings.Provider) {
	if r == nil {
		return nil, nil
	}
	if r.Live != nil {
		snapshot := r.Live.Snapshot()
		return snapshot.NoteIndex, snapshot.NoteProvider
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.NoteIndex, r.NoteProvider
}

func (r *Runtime) SetNoteEmbeddings(index embeddings.Index, provider embeddings.Provider) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.NoteIndex = index
	r.NoteProvider = provider
}

func (r *Runtime) CodeEmbeddings() (codeemb.Index, embeddings.Provider) {
	if r == nil {
		return nil, nil
	}
	if r.Live != nil {
		snapshot := r.Live.Snapshot()
		return snapshot.CodeIndex, snapshot.CodeProvider
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.CodeIndex, r.CodeProvider
}

func (r *Runtime) SetCodeEmbeddings(index codeemb.Index, provider embeddings.Provider) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.CodeIndex = index
	r.CodeProvider = provider
}

func (r *Runtime) Intel() *semdb.Store {
	if r == nil {
		return nil
	}
	if r.Live != nil {
		return r.Live.Snapshot().IntelStore
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.IntelStore
}

func (r *Runtime) SetIntelStore(store *semdb.Store) {
	if r == nil {
		return
	}
	var closeOld *semdb.Store
	r.mu.Lock()
	if r.ownsIntel && r.IntelStore != nil && r.IntelStore != store {
		closeOld = r.IntelStore
		r.ownsIntel = false
	}
	r.IntelStore = store
	r.mu.Unlock()
	if closeOld != nil {
		_ = closeOld.Close()
	}
}

func (r *Runtime) IgnoreMatcher() *ignore.Matcher {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Ignore
}

func (r *Runtime) SetIgnoreMatcher(matcher *ignore.Matcher) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Ignore = matcher
}

func (r *Runtime) WatchHubRef() *watchhub.Hub {
	if r == nil {
		return nil
	}
	if r.Live != nil {
		return r.Live.Snapshot().WatchHub
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.WatchHub
}

func (r *Runtime) SetWatchHub(hub *watchhub.Hub) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.Live == nil {
		r.WatchHub = hub
	}
	listeners := append([]func(*watchhub.Hub){}, r.hubListeners...)
	r.mu.Unlock()
	for _, listener := range listeners {
		if listener != nil {
			listener(hub)
		}
	}
}

// CapabilityView returns the live view when this runtime is backed by
// LiveRuntime. Standalone/test runtimes continue to use their owned fields.
func (r *Runtime) CapabilityView() runtimeview.View {
	if r == nil {
		return nil
	}
	return r.Live
}

func (r *Runtime) OnWatchHubSet(listener func(*watchhub.Hub)) {
	if r == nil || listener == nil {
		return
	}
	r.mu.Lock()
	r.hubListeners = append(r.hubListeners, listener)
	hub := r.WatchHub
	if r.Live != nil {
		hub = r.Live.Snapshot().WatchHub
	}
	r.mu.Unlock()
	if hub != nil {
		listener(hub)
	}
}

func (r *Runtime) SetLiveHealthProvider(provider interface {
	LiveHealth() bootstrap.LiveHealth
}) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.liveHealth = provider
}

func (r *Runtime) LiveHealth() *bootstrap.LiveHealth {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	provider := r.liveHealth
	r.mu.RUnlock()
	if provider == nil {
		return nil
	}
	health := provider.LiveHealth()
	return &health
}
