package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// ErrStartCancelled means a stop request overtook startup before publication.
var ErrStartCancelled = errors.New("runtime start was cancelled by stop")

// StartIntent is written before a detached child is launched, and retained
// until its runtime has finished shutting down. Stop can therefore find both
// a child that has not elected and an owner that has not published a manifest.
type StartIntent struct {
	VaultPath string `json:"vaultPath"`
	Token     string `json:"token"`
	ParentPID int    `json:"parentPid,omitempty"`
	OwnerPID  int    `json:"ownerPid,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

func (r *Registry) intentPath(vaultPath string) string {
	return filepath.Join(r.dir, InstanceID(vaultPath)+".pending.json")
}

func (r *Registry) intentGatePath(vaultPath string) string {
	return r.intentGatePathForID(InstanceID(vaultPath))
}

func (r *Registry) intentGatePathForID(instanceID string) string {
	return filepath.Join(r.dir, instanceID+".intent.lock")
}

// withIntentGate serializes reads and cancellation with election and publish.
// Process-lifetime semantics prevent reclaiming a paused live holder by age.
func (r *Registry) withIntentGate(ctx context.Context, vaultPath string, fn func() error) error {
	return r.withIntentGateForID(ctx, InstanceID(vaultPath), fn)
}

func (r *Registry) withIntentGateForID(ctx context.Context, instanceID string, fn func() error) error {
	for {
		release, acquired, err := indexlock.TryAcquireWithOptions(r.intentGatePathForID(instanceID), indexlock.AcquireOptions{
			Role: "runtime/intent", ProcessLifetime: true,
		})
		if err != nil {
			return err
		}
		if acquired {
			defer func() { _ = release() }()
			return fn()
		}
		if !sleepCtx(ctx, 50*time.Millisecond) {
			return ctx.Err()
		}
	}
}

func (r *Registry) readIntent(vaultPath string) (StartIntent, error) {
	data, err := os.ReadFile(r.intentPath(vaultPath))
	if err != nil {
		return StartIntent{}, err
	}
	var intent StartIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return StartIntent{}, err
	}
	if InstanceID(intent.VaultPath) != InstanceID(vaultPath) || intent.Token == "" {
		return StartIntent{}, fmt.Errorf("invalid runtime start intent for %s", vaultPath)
	}
	return intent, nil
}

func (r *Registry) writeIntent(intent StartIntent) error {
	data, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	path := r.intentPath(intent.VaultPath)
	return writeFileReplace(path, data)
}

// StartIntentFor reads a start intent under the same gate as replacements.
// On Windows, a plain open reader prevents an atomic rename over the file.
func (r *Registry) StartIntentFor(ctx context.Context, vaultPath string) (StartIntent, bool, error) {
	if err := ctx.Err(); err != nil {
		return StartIntent{}, false, err
	}
	var intent StartIntent
	err := r.withIntentGate(ctx, vaultPath, func() error {
		var readErr error
		intent, readErr = r.readIntent(vaultPath)
		return readErr
	})
	if os.IsNotExist(err) {
		return StartIntent{}, false, nil
	}
	return intent, err == nil, err
}

// CreateSpawnIntent must finish before spawnHeadlessWithToken starts the child.
func (r *Registry) CreateSpawnIntent(ctx context.Context, vaultPath, token string) (bool, error) {
	created := false
	err := r.withIntentGate(ctx, vaultPath, func() error {
		// An attached start may have elected since Ensure's last owner probe.
		// Never replace that owner's intent: doing so would cancel its watcher.
		if _, live := LockOwner(vaultPath); live {
			return nil
		}
		if err := r.writeIntent(StartIntent{VaultPath: vaultPath, Token: token, ParentPID: os.Getpid()}); err != nil {
			return err
		}
		created = true
		return nil
	})
	return created, err
}

// ElectIntent records the owner before startup recovery. A detached child
// must still own its parent's token; an attached serve creates its own intent.
func (r *Registry) ElectIntent(ctx context.Context, vaultPath, token string, spawned bool, manifest InstanceManifest) error {
	return r.withIntentGate(ctx, vaultPath, func() error {
		var intent StartIntent
		if spawned {
			current, err := r.readIntent(vaultPath)
			if err != nil || current.Token != token || current.Cancelled {
				return ErrStartCancelled
			}
			intent = current
		} else {
			intent = StartIntent{Token: token}
		}
		intent.VaultPath = vaultPath
		intent.OwnerPID = os.Getpid()
		if err := r.writeIntent(intent); err != nil {
			return err
		}
		return r.Write(manifest)
	})
}

// CancelIntent marks only the token observed while holding the gate. A later
// start gets a new token and is not affected by this stop request.
func (r *Registry) CancelIntent(ctx context.Context, vaultPath, expectedToken string) (StartIntent, bool, error) {
	var intent StartIntent
	var found bool
	err := r.withIntentGate(ctx, vaultPath, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		intent, err = r.readIntent(vaultPath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if intent.Token != expectedToken {
			return nil
		}
		found = true
		if intent.Cancelled {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		intent.Cancelled = true
		return r.writeIntent(intent)
	})
	return intent, found, err
}

// PublishManifestWithIntent fences publication against cancellation. Stop
// either wins the gate and cancels, or sees the published owner's lock.
func (r *Registry) PublishManifestWithIntent(ctx context.Context, vaultPath, token string, manifest InstanceManifest) error {
	return r.withIntentGate(ctx, vaultPath, func() error {
		intent, err := r.readIntent(vaultPath)
		if err != nil || intent.Token != token || intent.Cancelled {
			return ErrStartCancelled
		}
		return WriteManifest(vaultPath, manifest)
	})
}

// ClearIntent removes only this invocation's record, including after a failed
// elected startup. A successor's record must survive an older owner's defer.
func (r *Registry) ClearIntent(ctx context.Context, vaultPath, token string) error {
	return r.withIntentGate(ctx, vaultPath, func() error {
		intent, err := r.readIntent(vaultPath)
		if os.IsNotExist(err) || (err == nil && intent.Token != token) {
			return nil
		}
		if err != nil {
			return err
		}
		err = os.Remove(r.intentPath(vaultPath))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	})
}

// ClearEarlyRegistration handles a barrier failure after NewLiveRuntime has
// already released its election lock. It cannot delete a successor's record.
func (r *Registry) ClearEarlyRegistration(ctx context.Context, vaultPath, token string) error {
	return r.withIntentGate(ctx, vaultPath, func() error {
		path := r.Path(InstanceID(vaultPath))
		data, err := fileio.ReadFile(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		var manifest InstanceManifest
		if err := json.Unmarshal(data, &manifest); err != nil || manifest.RunID != token {
			return nil
		}
		err = os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	})
}

// ListIntents supplies stop --all with owners that have not yet published.
// A failed read returns the intents already discovered along with the error.
func (r *Registry) ListIntents(ctx context.Context) ([]StartIntent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, err
	}
	var intents []StartIntent
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return intents, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pending.json") {
			continue
		}
		instanceID := strings.TrimSuffix(entry.Name(), ".pending.json")
		var data []byte
		err := r.withIntentGateForID(ctx, instanceID, func() error {
			var readErr error
			data, readErr = os.ReadFile(filepath.Join(r.dir, entry.Name()))
			return readErr
		})
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return intents, fmt.Errorf("read runtime start intent %s: %w", entry.Name(), err)
		}
		var intent StartIntent
		if json.Unmarshal(data, &intent) == nil && intent.VaultPath != "" && intent.Token != "" {
			intents = append(intents, intent)
		}
	}
	return intents, nil
}
