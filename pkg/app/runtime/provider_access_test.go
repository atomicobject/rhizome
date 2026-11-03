package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeSupportsOlderHealthWithoutProviderAccess(t *testing.T) {
	manifest := testManifest(t, t.TempDir(), os.Getpid())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"instanceId": manifest.InstanceID, "runId": manifest.RunID, "pid": manifest.PID, "ready": true,
		}))
	}))
	t.Cleanup(server.Close)
	manifest.HTTPURL = server.URL
	health, err := Probe(context.Background(), manifest)
	require.NoError(t, err)
	require.Nil(t, health.ProviderAccess, "missing reports mean unknown; do not synthesize a source")
}
