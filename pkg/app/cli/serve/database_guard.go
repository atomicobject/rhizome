package serve

import (
	"fmt"
	"os"
	"sync"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
)

// databaseGuard notices when the index database on disk stops being the file
// this runtime opened (deleted, renamed away, or replaced by an in-process
// rebuild that bypassed the runtime). Every store handle in the process points
// at the old inode, so continuing would index into a file nobody else reads.
// The guard fails the job and asks the runtime to shut down; the next client
// starts a runtime against the new file.
type databaseGuard struct {
	path      string
	vaultRoot string
	shutdown  func(reason string)

	mu       sync.Mutex
	opened   os.FileInfo
	reported bool
}

// preflight is the lane preflight. The first job with an existing database
// records its identity; later jobs compare against it.
func (g *databaseGuard) preflight(lane.Kind) error {
	current, err := os.Stat(g.path)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.opened == nil {
		if err == nil {
			g.opened = current
		}
		return nil
	}
	if err == nil && os.SameFile(g.opened, current) {
		return nil
	}
	if !g.reported {
		g.reported = true
		g.shutdown(g.reason())
	}
	return lane.ErrDatabaseReplaced
}

// reason names what actually happened: a vault removed wholesale takes the
// database with it, and the root watcher may not have polled yet.
func (g *databaseGuard) reason() string {
	if g.vaultRoot != "" {
		if info, err := os.Stat(g.vaultRoot); err != nil || !info.IsDir() {
			return "vault root " + g.vaultRoot + " is gone"
		}
	}
	return fmt.Sprintf("index database %s was replaced on disk", g.path)
}

// installDatabaseGuard wires the guard onto the runtime's lane when the lane
// supports preflight.
func installDatabaseGuard(rt *bootstrap.LiveRuntime, vaultRoot, path string, shutdown func(reason string)) {
	if rt == nil || path == "" {
		return
	}
	setter, ok := rt.Lane().(interface{ SetPreflight(func(lane.Kind) error) })
	if !ok {
		return
	}
	guard := &databaseGuard{path: path, vaultRoot: vaultRoot, shutdown: shutdown}
	// Record the database this runtime opened now, not at the first job: a
	// replacement between startup and the first preflight must be noticed.
	if info, err := os.Stat(path); err == nil {
		guard.opened = info
	}
	setter.SetPreflight(guard.preflight)
}
