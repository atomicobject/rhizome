//go:build !windows

package desktop

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestCredentialCancelStopsOpAndChildWithoutSaving(t *testing.T) {
	dir := t.TempDir()
	env := testkit.Env(t)
	env.CLIConfigFile = filepath.Join(dir, "config.yml")
	env.LookupEnv = func(key string) (string, bool) {
		if key == "PATH" {
			return dir, true
		}
		return "", false
	}
	source := &repoconfig.CredentialSource{Account: "example.1password.com", Environment: strings.Repeat("a", 26), Keys: []string{"VOYAGE_API_KEY"}}
	require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{DefaultVaultName: "existing", Env: map[string]string{"VOYAGE_API_KEY": "synthetic-existing"}, CredentialSource: source}))
	before, err := os.ReadFile(env.CLIConfigFile)
	require.NoError(t, err)
	stat, err := os.Stat(env.CLIConfigFile)
	require.NoError(t, err)
	binary := filepath.Join(dir, "op")
	require.NoError(t, os.WriteFile(binary, []byte(`#!/bin/sh
if [ "$1" = '--version' ]; then printf '2.41.0'; exit; fi
dir="${0%/*}"
printf '%s' "$$" > "$dir/op.pid"
/bin/sleep 30 &
printf '%s' "$!" > "$dir/child.pid"
wait
printf '{"VOYAGE_API_KEY":"synthetic-late"}'
`), 0700))
	marker := filepath.Join(dir, "credential-import-1-1")
	require.NoError(t, os.WriteFile(marker, nil, 0600))
	service := &Service{stateDir: dir, env: env}
	result := make(chan error, 1)
	go func() {
		_, err := service.ImportCredentials(context.Background(), Request{Source: source, ImportID: "1-1"})
		result <- err
	}()
	var opPID, childPID int
	for _, item := range []struct {
		file string
		pid  *int
	}{{"op.pid", &opPID}, {"child.pid", &childPID}} {
		require.Eventually(t, func() bool {
			data, err := os.ReadFile(filepath.Join(dir, item.file))
			if err != nil {
				return false
			}
			*item.pid, _ = strconv.Atoi(string(data))
			return *item.pid > 0
		}, time.Second, 5*time.Millisecond)
	}
	// Always clean up the fixture even when the regression assertion fails.
	defer syscall.Kill(opPID, syscall.SIGKILL)
	defer syscall.Kill(childPID, syscall.SIGKILL)
	require.NoError(t, os.Remove(marker))
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not settle in two seconds")
	}
	require.Eventually(t, func() bool { return syscall.Kill(opPID, 0) != nil && syscall.Kill(childPID, 0) != nil }, time.Second, 10*time.Millisecond, "op and its child must both exit")
	after, err := os.ReadFile(env.CLIConfigFile)
	require.NoError(t, err)
	require.True(t, bytes.Equal(before, after))
	afterStat, err := os.Stat(env.CLIConfigFile)
	require.NoError(t, err)
	require.Equal(t, stat.ModTime(), afterStat.ModTime())
	status, err := service.CredentialStatus()
	require.NoError(t, err)
	require.Equal(t, source, status.Source)
	require.Equal(t, []string{"VOYAGE_API_KEY"}, status.Ready)
}

func TestCredentialCancelAtSaveBoundaryAndStatusAfterCommit(t *testing.T) {
	for _, cancelBeforeSave := range []bool{true, false} {
		t.Run(strconv.FormatBool(cancelBeforeSave), func(t *testing.T) {
			dir := t.TempDir()
			env := testkit.Env(t)
			env.CLIConfigFile = filepath.Join(dir, "config.yml")
			env.LookupEnv = func(key string) (string, bool) {
				if key == "PATH" {
					return dir, true
				}
				return "", false
			}
			oldSource := &repoconfig.CredentialSource{Account: "old.1password.com", Environment: strings.Repeat("a", 26), Keys: []string{"VOYAGE_API_KEY"}}
			newSource := *oldSource
			newSource.Account = "new.1password.com"
			newSource.Keys = []string{"VOYAGE_API_KEY", "TYPESAFE_API_KEY"}
			require.NoError(t, repoconfig.SaveCliConfig(env, repoconfig.CliConfig{DefaultVaultName: "existing", Env: map[string]string{"VOYAGE_API_KEY": "synthetic-old"}, CredentialSource: oldSource}))
			before, err := os.ReadFile(env.CLIConfigFile)
			require.NoError(t, err)
			beforeStat, err := os.Stat(env.CLIConfigFile)
			require.NoError(t, err)
			marker := filepath.Join(dir, "credential-import-2-1")
			require.NoError(t, os.WriteFile(marker, nil, 0600))
			body := `#!/bin/sh
if [ "$1" = '--version' ]; then printf '2.41.0'; exit; fi
`
			if cancelBeforeSave {
				body += `/bin/unlink "${0%/*}/credential-import-2-1"
`
			}
			body += `printf '{"VOYAGE_API_KEY":"synthetic-new","TYPESAFE_API_KEY":"synthetic-typesafe"}'
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "op"), []byte(body), 0700))
			service := &Service{stateDir: dir, env: env}
			_, err = service.ImportCredentials(context.Background(), Request{Source: &newSource, ImportID: "2-1"})
			after, readErr := os.ReadFile(env.CLIConfigFile)
			require.NoError(t, readErr)
			if cancelBeforeSave {
				require.Error(t, err)
				require.Equal(t, before, after)
				afterStat, err := os.Stat(env.CLIConfigFile)
				require.NoError(t, err)
				require.Equal(t, beforeStat.ModTime(), afterStat.ModTime())
			} else {
				require.NoError(t, err)
				require.NoError(t, os.Remove(marker))
			}
			cfg, err := repoconfig.LoadCliConfig(env, false)
			require.NoError(t, err)
			status, err := service.CredentialStatus()
			require.NoError(t, err)
			require.Equal(t, "existing", cfg.DefaultVaultName)
			if cancelBeforeSave {
				require.Equal(t, oldSource, status.Source)
				require.Equal(t, "synthetic-old", cfg.Env["VOYAGE_API_KEY"])
				require.NotContains(t, cfg.Env, "TYPESAFE_API_KEY")
			} else {
				require.Equal(t, &newSource, status.Source)
				require.Equal(t, "synthetic-new", cfg.Env["VOYAGE_API_KEY"])
				require.Contains(t, status.Ready, "TYPESAFE_API_KEY")
			}
		})
	}
}

func TestCredentialMissingOrUnreadableMarkerCancelsBeforeOp(t *testing.T) {
	dir := t.TempDir()
	source := &repoconfig.CredentialSource{Account: "example.1password.com", Environment: strings.Repeat("a", 26), Keys: []string{"VOYAGE_API_KEY"}}
	service := &Service{stateDir: dir, env: testkit.Env(t)}
	_, err := service.ImportCredentials(context.Background(), Request{Source: source, ImportID: "3-1"})
	require.Equal(t, "credential_canceled", asProblem(err).Code)
	marker := filepath.Join(dir, "credential-import-3-1")
	require.NoError(t, os.WriteFile(marker, nil, 0000))
	_, err = service.ImportCredentials(context.Background(), Request{Source: source, ImportID: "3-1"})
	require.Equal(t, "credential_canceled", asProblem(err).Code)
}
