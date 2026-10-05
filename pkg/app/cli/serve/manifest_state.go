package serve

import (
	"sync"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
)

// manifestState is the single source of truth for what this runtime publishes.
// The manifest file, the global registry entry, and the health endpoint are all
// projections of it, so a client can never see three different answers.
type manifestState struct {
	mu       sync.Mutex
	manifest appruntime.InstanceManifest
}

func newManifestState(manifest appruntime.InstanceManifest) *manifestState {
	return &manifestState{manifest: manifest}
}

func (s *manifestState) Snapshot() appruntime.InstanceManifest {
	if s == nil {
		return appruntime.InstanceManifest{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.manifest
}

// SetAddress completes the manifest once the listener is bound. The manifest is
// only published after this, so a client that reads it can always connect.
func (s *manifestState) SetAddress(host string, port int, url string) appruntime.InstanceManifest {
	if s == nil {
		return appruntime.InstanceManifest{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manifest.HTTPHost = host
	s.manifest.HTTPPort = port
	s.manifest.HTTPURL = url
	return s.manifest
}

// SetReady is monotonic: readiness is a one-way gate, and a later snapshot must
// never demote a runtime that already answered ready.
func (s *manifestState) SetReady(ready bool) appruntime.InstanceManifest {
	if s == nil {
		return appruntime.InstanceManifest{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ready {
		s.manifest.Ready = true
	}
	return s.manifest
}
