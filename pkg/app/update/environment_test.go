package update

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func TestRunDefaultsToInvocationWorkingDirectory(t *testing.T) {
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			repo, global, target := updateExecutionFixture(t, "v0.50.0")
			deps := executionDependencies()
			var replaced []string
			deps.replace = func(_, destination string) error {
				replaced = append(replaced, destination)
				return nil
			}
			ctx := vaultconfig.WithEnv(context.Background(), vaultconfig.Env{WorkingDir: repo})
			result, err := Run(ctx, Options{Pinned: true, ExecutablePath: global, deps: &deps})
			require.NoError(t, err)
			require.Equal(t, []string{target}, replaced)
			require.Equal(t, []TargetResult{{Role: UpdateTargetRepo, Path: target, Version: "v0.50.0", BinaryUpdated: true, MarkerWritten: true}}, result.Targets)
		})
	}
}

func TestRunRejectsUnavailableInvocationWorkingDirectory(t *testing.T) {
	ctx := vaultconfig.WithEnv(context.Background(), vaultconfig.Env{})
	_, err := Run(ctx, Options{Pinned: true})
	require.ErrorContains(t, err, "working directory must be absolute")
}

func TestRunAppUpdateUsesInvocationFeed(t *testing.T) {
	t.Setenv("RZM_DESKTOP_UPDATE_FEED", "https://host.invalid/feed.json")
	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			bundle, link := appUpdateFixture(t, false)
			endpoint := "https://" + name + ".invalid/feed.json"
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, endpoint, request.URL.String())
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"version":"1.0.0"}`)), Header: make(http.Header)}, nil
			})}
			ctx := vaultconfig.WithEnv(context.Background(), vaultconfig.Env{LookupEnv: func(key string) (string, bool) {
				return endpoint, key == "RZM_DESKTOP_UPDATE_FEED"
			}})
			result, err := Run(ctx, Options{ExecutablePath: link, WorkDir: t.TempDir(), HTTPClient: client})
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, []TargetResult{{Role: UpdateTargetGlobal, Path: bundle.Root, Version: "v1.0.0"}}, result.Targets)
		})
	}
}
