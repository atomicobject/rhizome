package indexing

import (
	"context"
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

func runUnifiedCoreForTest(t testing.TB, ctx context.Context, opts UnifiedOptions) error {
	t.Helper()
	lease, acquired, err := indexlock.TryAcquireLease(opts.VaultPath, indexlock.AcquireOptions{Role: "test/unified-index"})
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("test vault indexing lease is already held")
	}
	defer func() {
		if err := lease.Release(); err != nil {
			t.Errorf("release test indexing lease: %v", err)
		}
	}()
	return RunUnifiedCore(ctx, lease, opts)
}
