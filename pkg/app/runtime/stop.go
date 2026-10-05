package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// exitPoll is how often RequestShutdown re-checks that a runtime is gone.
const exitPoll = 50 * time.Millisecond

// RequestShutdown asks a runtime to exit gracefully and waits for it to go
// away, up to grace. A non-nil error means the runtime is still there: the
// caller decides what to do about it, because a headless runtime may be
// terminated (TerminateHeadless) and an attached one never may.
func RequestShutdown(ctx context.Context, c *Client, grace time.Duration) error {
	requestCtx, cancel := context.WithTimeout(ctx, grace)
	defer cancel()
	req, err := c.NewRequest(requestCtx, http.MethodPost, ShutdownPath, map[string]string{"reason": "client request"})
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	switch {
	case err != nil && !transportFailure(err):
		return err
	case err == nil:
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= http.StatusBadRequest {
			return statusError(resp, "shut down runtime")
		}
	}
	// A refused connection means it is already gone; confirm either way.
	if err := WaitForExit(requestCtx, c.Manifest, grace); err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return fmt.Errorf("runtime pid %d did not exit within %s: %w", c.Manifest.PID, grace, err)
		}
		return err
	}
	return nil
}

// WaitForExit waits until the runtime is gone: first its listener stops
// answering, then its process exits. The listener closes before the process
// finishes (closing stores, then releasing runtime.lock last), so a successor
// that starts on the listener signal alone loses election to the still-closing
// owner and a rebuild would unlink a database still held open. The PID check
// is skipped for a manifest naming this process (in-process test doubles).
func WaitForExit(ctx context.Context, manifest InstanceManifest, grace time.Duration) error {
	deadline := time.Now().Add(grace)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := Probe(ctx, manifest); err != nil && !errors.Is(err, ErrRuntimeUnresponsive) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("runtime pid %d did not exit within %s", manifest.PID, grace)
		}
		if !sleepCtx(ctx, exitPoll) {
			return ctx.Err()
		}
	}
	// Gone means the process exited, or it released runtime.lock (the last
	// thing a runtime does on the way out; a crashed owner leaves a lock that
	// election reclaims by dead PID, so a dead PID is also enough).
	lockPath := LockPath(manifest.VaultPath)
	for {
		if manifest.PID > 0 && manifest.PID != os.Getpid() && !pidExists(manifest.PID) {
			return nil
		}
		if lock, ok := indexlock.ReadLockData(lockPath); ok && lock.PID != manifest.PID {
			return nil
		}
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("runtime pid %d stopped listening but did not release %s within %s", manifest.PID, lockPath, grace)
		}
		if !sleepCtx(ctx, exitPoll) {
			return ctx.Err()
		}
	}
}

// TerminateHeadless force-stops a headless runtime that ignored a graceful
// shutdown. An attached runtime belongs to the human who started it and must
// never be passed here. The kill is sent only while the vault's runtime lock
// still names the manifest's PID: a runtime removes that lock as it exits, so
// a PID the OS has since reused for an unrelated process is never signalled.
func TerminateHeadless(vaultPath string, manifest InstanceManifest) error {
	if manifest.Mode != ModeHeadless {
		return fmt.Errorf("runtime pid %d is %s; only headless runtimes are terminated", manifest.PID, manifest.Mode)
	}
	if !runtimeLockNames(vaultPath, manifest.PID) {
		// Already gone (or someone else owns the vault now): nothing to kill.
		return nil
	}
	proc, err := os.FindProcess(manifest.PID)
	if err != nil {
		return err
	}
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// runtimeLockNames reports whether the vault's runtime lock currently names pid.
func runtimeLockNames(vaultPath string, pid int) bool {
	lock, ok := indexlock.ReadLockData(LockPath(vaultPath))
	return ok && lock.PID == pid
}
