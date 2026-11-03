package provideraccess

import (
	"context"
	"sync"
	"time"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

// CachedRuntimeReports keeps health independent of configuration and keychain
// I/O. A health read returns the latest immutable snapshot and starts at most
// one refresh every five seconds. Refreshes never construct provider clients.
func (f Factory) CachedRuntimeReports(ctx context.Context, env vaultconfig.Env) func() []RuntimeReport {
	if ctx == nil {
		ctx = context.Background()
	}
	var mu sync.Mutex
	var refreshing bool
	var updated time.Time
	latest := []RuntimeReport{
		{Provider: "voyage", Next: Report{Provider: "voyage", Source: SourceNone, Reason: "checking"}},
		{Provider: "typesafe", Next: Report{Provider: "typesafe", Source: SourceNone, Reason: "checking"}},
	}
	return func() []RuntimeReport {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() == nil && !refreshing && time.Since(updated) >= 5*time.Second {
			refreshing = true
			go func() {
				// The loader bounds its own I/O. This work touches no runtime
				// stores or clients, so shutdown need not wait on a locked keychain.
				reports := f.RuntimeReports(env)
				mu.Lock()
				defer mu.Unlock()
				if ctx.Err() == nil {
					latest = reports
				}
				updated, refreshing = time.Now(), false
			}()
		}
		return latest
	}
}
