package bootstrap

import (
	"errors"
	"log"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Election: exactly one runtime per vault root owns .rhizome/runtime.lock
// (SPEC-0104 US1). The owner runs the watcher, the indexing lane, and the
// control API. Election is synchronous inside NewLiveRuntime so `rzm serve`
// knows before it binds a listener whether it may serve this vault; a loser
// exits with appruntime.ExitCodeAlreadyRunning without touching the winner's
// manifest, lock, or log.
//
// Runtimes that disable leader work (one-shot CLI readers) never elect and
// never become owner: they only read the shared index.

// ErrNotRuntimeOwner reports that another process owns this vault's runtime.
// Every capability of a losing runtime fails with it.
var ErrNotRuntimeOwner = errors.New("another runtime owns this vault")

// failAllCapabilities publishes err on every capability gate so no WaitFor call
// can block on initialization that will never run.
func (rt *LiveRuntime) failAllCapabilities(err error) {
	rt.searchErr.Store(&err)
	rt.semanticErr.Store(&err)
	rt.codeErr.Store(&err)
	close(rt.searchReady)
	close(rt.semanticReady)
	close(rt.codeReady)
	close(rt.sessionReady)
}

// electRuntimeOwner runs once, synchronously, from NewLiveRuntime. It reports
// whether this process owns the vault. A lock error is treated as a loss so
// two processes can never both believe they own the vault.
func (rt *LiveRuntime) electRuntimeOwner() bool {
	if rt.disableLeaderWork {
		return false
	}
	// A serve from before SPEC-0104 elected on watcher.lock and knows nothing
	// about runtime.lock. Treat a live one as the owner so an upgrade never
	// runs two watchers on one vault; the operator stops it by hand.
	if pid, live := appruntime.LegacyOwner(rt.VaultPath); live {
		if rt.debug {
			log.Printf("live: older Rhizome serve (pid %d) owns this vault", pid)
		}
		return false
	}
	acquired, err := rt.tryHoldRuntimeLock()
	if err != nil {
		if rt.debug {
			log.Printf("live: runtime lock error: %v", err)
		}
		return false
	}
	// A runtime that is exiting closes its listener first and releases the
	// lock last. Losing to that window (idle exit, `rzm stop`, the database
	// guard) is routine, so wait it out rather than exit 3 or make a client
	// burn its whole spawn budget.
	for deadline := time.Now().Add(appruntime.StopGrace); !acquired && appruntime.OwnerIsExiting(rt.VaultPath) && time.Now().Before(deadline); {
		time.Sleep(100 * time.Millisecond)
		if acquired, err = rt.tryHoldRuntimeLock(); err != nil {
			return false
		}
	}
	if acquired {
		appruntime.RemoveLegacyFiles(rt.VaultPath)
		rt.becomeLeader()
	}
	return acquired
}

// tryHoldRuntimeLock acquires the election lock once and, on success, keeps
// it heartbeating for the runtime's lifetime.
func (rt *LiveRuntime) tryHoldRuntimeLock() (bool, error) {
	lockPath := appruntime.LockPath(rt.VaultPath)
	release, acquired, err := indexlock.TryAcquireWithOptions(lockPath, indexlock.AcquireOptions{Role: "runtime/election", ProcessLifetime: true})
	if err != nil || !acquired {
		return false, err
	}
	rt.lockRelease = release
	stopHeartbeat := indexlock.StartHeartbeat(rt.ctx, lockPath, 30*time.Second)
	rt.addCloser(func() {
		// A caller's finalization callback must not strand election ownership,
		// including if it panics while unwinding a failed startup.
		defer func() {
			if rt.lockRelease != nil {
				if err := rt.lockRelease(); err != nil && rt.debug {
					log.Printf("live: runtime lock release failed: %v", err)
				}
			}
		}()
		stopHeartbeat()
		if rt.beforeOwnershipRelease != nil {
			rt.beforeOwnershipRelease()
		}
	})
	return true, nil
}

// ElectionWon reports whether this runtime owns the vault's runtime lock. It
// is valid as soon as NewLiveRuntime returns, so the serve command can decide
// to exit before binding its listener. A runtime with leader work disabled
// never wins.
func (rt *LiveRuntime) ElectionWon() bool {
	if rt == nil {
		return false
	}
	return rt.leaderFlag.Load()
}

// IsLeader reports whether this runtime owns the vault.
func (rt *LiveRuntime) IsLeader() bool {
	return rt.leaderFlag.Load()
}

func (rt *LiveRuntime) becomeLeader() {
	rt.leaderOnce.Do(func() {
		// Install the indexing lane at promotion so the control API can accept
		// explicit index jobs as soon as the listener is up, before leader
		// indexing work starts.
		if rt.Lane() == nil {
			indexLane := lane.New(lane.Options{LockPath: obsidian.IndexLockPath(rt.VaultPath), Debug: rt.debug})
			rt.SetLane(indexLane)
			rt.addCloser(indexLane.Close)
		}
		rt.leaderFlag.Store(true)
		close(rt.leaderCh)
	})
}

type laneHolder struct{ lane lane.Lane }

// SetLane installs the serialized indexing executor. Call it before leader
// work starts; later calls replace the executor for the next job.
func (rt *LiveRuntime) SetLane(l lane.Lane) {
	if rt == nil || l == nil {
		return
	}
	rt.indexLane.Store(&laneHolder{lane: l})
}

// Lane returns the installed indexing lane, or nil before installation.
func (rt *LiveRuntime) Lane() lane.Lane {
	if rt == nil {
		return nil
	}
	if h := rt.indexLane.Load(); h != nil {
		return h.lane
	}
	return nil
}
