package cmd

import "os"

import "github.com/atomicobject/rhizome/internal/testkit"

import (
	"bytes"
	"context"
	"errors"
	"testing"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/stretchr/testify/require"
)

func TestUpdateCommandPrintsCompletedTargetsBeforeReturningError(t *testing.T) {
	rootCmd := newRootCommand(commandEnv{Getwd: os.Getwd, Config: testkit.Env(t)})
	updateCmd, _, _ := rootCmd.Find([]string{"update"})

	wantErr := errors.New("write repo version marker")
	originalRunUpdate := runUpdate
	runUpdate = func(context.Context, appupdate.Options) (appupdate.Result, error) {
		return appupdate.Result{Targets: []appupdate.TargetResult{
			{
				Role:          appupdate.UpdateTargetGlobal,
				Path:          "/usr/local/bin/rzm",
				Version:       "v0.51.0",
				BinaryUpdated: true,
			},
			{
				Role:    appupdate.UpdateTargetRepo,
				Path:    "/workspace/.rhizome/bin/darwin-arm64/rzm",
				Version: "v0.51.0",
			},
		}}, wantErr
	}
	t.Cleanup(func() { runUpdate = originalRunUpdate })

	var stdout bytes.Buffer
	updateCmd.SetOut(&stdout)
	t.Cleanup(func() { updateCmd.SetOut(nil) })

	err := updateCmd.RunE(updateCmd, nil)

	require.ErrorIs(t, err, wantErr)
	require.Equal(t, "Updated global /usr/local/bin/rzm to Rhizome v0.51.0\n", stdout.String())
}
