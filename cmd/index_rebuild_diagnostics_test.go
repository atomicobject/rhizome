package cmd

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRebuildPublishesReportBeforeRuntimeRestart(t *testing.T) {
	resetIndexFlags(t)
	indexRebuild = true
	vault := newRuntimeTestVault(t)
	startFakeRuntimeServer(t, vault, nil)
	t.Setenv(appruntime.AutostartEnv, "0")
	recorder, err := diagnostics.Open(vault, diagnostics.Options{Stderr: io.Discard})
	require.NoError(t, err)
	defer recorder.Close()
	command := newIndexTestCommand(t, &bytes.Buffer{})
	command.SetContext(diagnostics.WithRecorder(command.Context(), recorder))
	err = runResolvedIndexCommand(command, obsidian.VaultDefinition{Path: vault}, func() (notemeta.Indexer, error) {
		return notemeta.Indexer{}, fmt.Errorf("stop before indexing")
	})
	require.ErrorContains(t, err, "stop before indexing")
	events, err := diagnostics.ReadEvents(vault, diagnostics.Filter{})
	require.NoError(t, err)
	var indexFinished, restartStarted uint64
	for _, event := range events.Events {
		if event.Name == "index.finished" {
			indexFinished = event.Sequence
		}
		if event.Subsystem == "runtime" && event.Name == "ensure.started" {
			restartStarted = event.Sequence
		}
	}
	require.NotZero(t, indexFinished)
	require.Greater(t, restartStarted, indexFinished)
	report, err := diagnostics.ReadLatest(vault, "index")
	require.NoError(t, err)
	require.Equal(t, "error", report.Status)
}
