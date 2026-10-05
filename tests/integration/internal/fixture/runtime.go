//go:build integration

package fixture

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/cli/serve"
)

// StopRuntime drains the fixture's runtime, including unpublished startup and
// shutdown already in progress, before its temporary vault can be removed.
func StopRuntime(t testing.TB, vault string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var output bytes.Buffer
	if err := serve.Stop(ctx, serve.StopOptions{VaultPath: vault, Grace: 5 * time.Second, Out: &output}); err != nil {
		t.Errorf("stop fixture runtime: %v\n%s", err, output.String())
	}
}
