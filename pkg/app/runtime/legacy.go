package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Files written by Rhizome before SPEC-0104. A pre-upgrade `rzm serve` elected
// on watcher.lock and published serve-dev.json; neither is read by the new
// runtime, so an old process could otherwise coexist with a new one on the
// same vault. LegacyOwner makes the old process visible to election and to
// clients; RemoveLegacyFiles clears the leftovers once a new runtime owns the vault.
const (
	legacyLockFileName      = "watcher.lock"
	legacyDiscoveryFileName = "serve-dev.json"
	legacyHintsLogFileName  = "cache.dirty.log"
)

// ErrLegacyRuntime reports that a Rhizome serve from before the runtime
// coordination change still owns the vault. It must be stopped by hand.
var ErrLegacyRuntime = errors.New("an older Rhizome serve owns this vault; stop it and rerun")

// legacyHeartbeatStale bounds how old a legacy lock's heartbeat may be. The
// old serve touched watcher.lock every 30 seconds; a lock untouched for longer
// than this belongs to a process that is gone even if its PID was reused.
const legacyHeartbeatStale = 5 * time.Minute

// LegacyOwner reports a live pre-SPEC-0104 serve process for the vault: its
// lock names a live PID and was heartbeated recently.
func LegacyOwner(vaultPath string) (pid int, live bool) {
	path := rhizomeFile(vaultPath, legacyLockFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > legacyHeartbeatStale {
		return 0, false
	}
	var lock struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(data, &lock) != nil || lock.PID <= 0 || !pidExists(lock.PID) {
		return 0, false
	}
	return lock.PID, true
}

// LegacyOwnerError wraps ErrLegacyRuntime with the owning PID and lock file
// when one is live.
func LegacyOwnerError(vaultPath string) error {
	if pid, live := LegacyOwner(vaultPath); live {
		return fmt.Errorf("%w (pid %d, %s)", ErrLegacyRuntime, pid, rhizomeFile(vaultPath, legacyLockFileName))
	}
	return nil
}

// RemoveLegacyFiles deletes pre-SPEC-0104 runtime files that no live process
// owns. Call it after winning election; it never removes a lock whose PID is
// alive.
func RemoveLegacyFiles(vaultPath string) {
	if _, live := LegacyOwner(vaultPath); !live {
		_ = os.Remove(rhizomeFile(vaultPath, legacyLockFileName))
	}
	_ = os.Remove(rhizomeFile(vaultPath, legacyDiscoveryFileName))
	_ = os.Remove(rhizomeFile(vaultPath, legacyDiscoveryFileName+".tmp"))
	matches, _ := filepath.Glob(rhizomeFile(vaultPath, legacyHintsLogFileName) + "*")
	for _, match := range matches {
		_ = os.Remove(match)
	}
}
