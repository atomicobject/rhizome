package ontology

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sourcetxn"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

func publishSourceEditStates(ctx context.Context, states map[string]*documentState, vault *paths.VaultPaths, writer *sourcetxn.Writer) error {
	intent := sourcetxn.Intent{}
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	var identity strings.Builder
	for _, name := range names {
		state := states[name]
		if state == nil {
			continue
		}
		absolute, err := vault.AbsNotePath(paths.NotePath(name))
		if err != nil {
			return err
		}
		info, err := os.Lstat(absolute.String())
		if err != nil {
			return &sourcetxn.SourceConflictError{Path: name, Err: err}
		}
		if !info.Mode().IsRegular() {
			return &sourcetxn.SourceConflictError{Path: name, Err: fmt.Errorf("source must remain a regular file")}
		}
		intent.Files = append(intent.Files, sourcetxn.FilePatch{Path: name, Original: []byte(state.originalContent), OriginalMode: info.Mode(), Updated: []byte(state.content)})
		fmt.Fprintf(&identity, "%s\x00%s\x00%s\x00%o\n", name, state.expectedCurrentFingerprint, hashText(state.content), info.Mode())
	}
	intent.Fingerprint = sourcetxn.Hash([]byte(identity.String()))
	return writer.Apply(ctx, intent)
}

func acquireSourceEditLease(ctx context.Context, root string, borrowed *indexlock.Lease, wait bool) (*indexlock.Lease, func() error, error) {
	if borrowed != nil {
		if err := borrowed.Validate(root); err != nil {
			return nil, nil, err
		}
		return borrowed, func() error { return nil }, nil
	}
	lockPath := filepath.Join(root, ".rhizome", "index.lock")
	var priority *indexlock.PriorityRequest
	defer func() { priority.Close() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		lease, acquired, err := indexlock.TryAcquireLease(root, indexlock.AcquireOptions{Role: "ontology/source-edit"})
		if err != nil {
			return nil, nil, err
		}
		if acquired {
			stop := lease.StartHeartbeat(ctx, 30*time.Second)
			return lease, func() error { stop(); return lease.Release() }, nil
		}
		if !wait {
			return nil, nil, fmt.Errorf("another vault writer is active")
		}
		if !priority.Active() {
			priority.Close()
			priority, err = indexlock.RequestPriority(lockPath)
			if err != nil {
				return nil, nil, err
			}
		}
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func recoverSourceEdits(ctx context.Context, root string, wait bool) error {
	vault, err := paths.NewVaultPaths(root)
	if err != nil {
		return err
	}
	lease, release, err := acquireSourceEditLease(ctx, vault.Root(), nil, wait)
	if err != nil {
		return err
	}
	writer, _, recoveryErr := sourcetxn.Open(vault.Root(), lease, sourcetxn.WriterOptions{Role: sourcetxn.SourceEditor})
	if writer != nil {
		writer.Close()
	}
	return errors.Join(recoveryErr, release())
}

// RecoverInterruptedEdits settles source writes while holding the vault writer lease.
func RecoverInterruptedEdits(vaultPath string) error {
	return recoverSourceEdits(context.Background(), vaultPath, false)
}

// RecoverInterruptedEditsContext waits cancellably for the prior writer before recovery.
func RecoverInterruptedEditsContext(ctx context.Context, vaultPath string) error {
	return recoverSourceEdits(ctx, vaultPath, true)
}
