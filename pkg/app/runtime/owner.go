package runtime

import (
	"context"
	"errors"
	"os"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// LockOwner reports the PID named by the vault's runtime lock and whether that
// process is alive. It is the operator-facing complement to the manifest: the
// lock is what election contends on, so when the manifest is missing or stale
// this is the process to name.
func LockOwner(vaultPath string) (pid int, live bool) {
	lock, ok := indexlock.ReadLockData(LockPath(vaultPath))
	if !ok || lock.PID <= 0 {
		return 0, false
	}
	return lock.PID, pidExists(lock.PID)
}

// OwnerIsExiting reports a lock held by a live process that no longer answers
// at its manifest: the runtime is between closing its listener and releasing
// the lock. Callers retry briefly instead of treating it as a live owner.
func OwnerIsExiting(vaultPath string) bool {
	pid, live := LockOwner(vaultPath)
	if !live || pid == os.Getpid() {
		return false
	}
	manifest, err := ReadManifest(vaultPath)
	if err != nil {
		return true
	}
	_, err = Probe(context.Background(), manifest)
	return errors.Is(err, ErrNoRuntime)
}
