package agentchat

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

// slowStatusHarness answers its status probe after a delay, or fails when the
// probe's context ends first.
type slowStatusHarness struct {
	harnesstest.Harness
}

func (*slowStatusHarness) Status(ctx context.Context) (harness.Status, error) {
	select {
	case <-time.After(50 * time.Millisecond):
		return harness.Status{Kind: harness.KindCodex, Installed: true, LoggedIn: true}, nil
	case <-ctx.Done():
		return harness.Status{}, ctx.Err()
	}
}

func TestSettingsStatusSurvivesACallerThatLeaves(t *testing.T) {
	env := setTestSettings(t, Settings{})
	service, err := NewService(vaultconfig.WithEnv(context.Background(), env), t.TempDir(),
		map[harness.Kind]harness.Harness{harness.KindCodex: &slowStatusHarness{}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = service.Close() })

	leaving, leave := context.WithCancel(context.Background())
	leave()
	_, err = service.Settings(leaving)
	require.NoError(t, err)

	response, err := service.Settings(context.Background())
	require.NoError(t, err)
	require.Equal(t, harness.KindCodex, response.Resolved.Harness)
	require.True(t, response.Harnesses[0].Status.Installed)
	require.Empty(t, response.Harnesses[0].Status.LastError)
}
