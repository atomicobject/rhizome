package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	diagnosticcli "github.com/atomicobject/rhizome/pkg/app/cli/diagnostics"
	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticsJSONAndReadOnly(t *testing.T) {
	root := t.TempDir()
	diagnosticDir := filepath.Join(root, ".rhizome", "diagnostics")
	require.NoError(t, os.MkdirAll(diagnosticDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("invalid: ["), 0600))
	report := evidence.Report{SchemaVersion: evidence.SchemaVersion, OperationID: "old-error", Kind: "index", Status: "error", StartedAt: time.Now().Add(-30 * 24 * time.Hour), FinishedAt: time.Now().Add(-30*24*time.Hour + time.Second), Error: "synthetic failure"}
	data, err := json.Marshal(report)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(diagnosticDir, "latest-index.json"), data, 0600))
	before := diagnosticTree(t, root)
	for _, args := range [][]string{{"index", "--last", "--json"}, {"logs", "--level", "warn", "--since", "2d", "--json"}, {"report", "missing", "--json"}} {
		t.Run(args[0], func(t *testing.T) {
			command := newDiagnosticsCmd()
			var stdout, stderr bytes.Buffer
			command.SetOut(&stdout)
			command.SetErr(&stderr)
			command.SilenceErrors = true
			command.SilenceUsage = true
			command.SetArgs(append(args, "--vault", root))
			err := command.Execute()
			var result diagnosticcli.Result
			decoder := json.NewDecoder(&stdout)
			require.NoError(t, decoder.Decode(&result))
			require.ErrorIs(t, decoder.Decode(new(any)), io.EOF)
			if args[0] == "report" {
				require.Error(t, err)
				require.NotEmpty(t, result.Error)
			} else {
				require.NoError(t, err)
			}
			if args[0] == "index" {
				require.Len(t, result.Reports, 1)
				require.Equal(t, "old-error", result.Reports[0].OperationID)
				require.Equal(t, "error", result.Reports[0].Status)
			}
			require.Empty(t, stderr.String())
			require.Equal(t, before, diagnosticTree(t, root), "offline reads must create no DB, runtime, or diagnostics artifacts")
		})
	}
}

// Execute the real root command in a fresh process so global logging/runtime
// hooks cannot be hidden by an isolated Cobra constructor.
func TestDiagnosticsRootProcessOffline(t *testing.T) {
	if os.Getenv("RZM_DIAGNOSTICS_TEST_CHILD") == "1" {
		os.Args = []string{"rzm", "diagnostics", "index", "--last", "--json", "--vault", os.Getenv("RZM_DIAGNOSTICS_TEST_VAULT")}
		os.Exit(Execute())
	}
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("bad: ["), 0600))
	before := diagnosticTree(t, root)
	child := exec.Command(os.Args[0], "-test.run=^TestDiagnosticsRootProcessOffline$")
	child.Dir = root
	child.Env = append(os.Environ(), "RZM_DIAGNOSTICS_TEST_CHILD=1", "RZM_DIAGNOSTICS_TEST_VAULT="+root, "RZM_SKIP_REPO_DELEGATE=1")
	var stdout, stderr bytes.Buffer
	child.Stdout = &stdout
	child.Stderr = &stderr
	require.NoError(t, child.Run(), stderr.String())
	var result diagnosticcli.Result
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result), stdout.String())
	require.Empty(t, result.Error)
	require.NotEmpty(t, result.Coverage.Warnings)
	require.Equal(t, before, diagnosticTree(t, root))
}

func diagnosticTree(t *testing.T, root string) map[string]string {
	t.Helper()
	tree := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			tree[path+string(os.PathSeparator)] = "directory"
		} else {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			tree[path] = string(data)
		}
		return nil
	}))
	return tree
}

func TestDiagnosticsHistoryFlagsDoNotSilentlySelectLatest(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "diagnostics")
	require.NoError(t, os.MkdirAll(dir, 0700))
	latest := evidence.Report{SchemaVersion: evidence.SchemaVersion, OperationID: "protected-latest", Kind: "index", Status: "success", FinishedAt: time.Now()}
	data, err := json.Marshal(latest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "latest-index.json"), data, 0600))
	for _, tc := range []struct {
		args  []string
		error bool
	}{
		{[]string{"index", "--until", "invalid"}, true},
		{[]string{"index", "--last", "--limit", "1"}, true},
		{[]string{"index", "--limit", "1"}, false},
		{[]string{"report"}, true},
	} {
		command := newDiagnosticsCmd()
		var stdout bytes.Buffer
		command.SetOut(&stdout)
		command.SetErr(io.Discard)
		command.SetArgs(append(tc.args, "--json", "--vault", root))
		err := command.Execute()
		var result diagnosticcli.Result
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result), stdout.String())
		if tc.error {
			require.Error(t, err)
			require.NotEmpty(t, result.Error)
		} else {
			require.NoError(t, err)
			require.Empty(t, result.Reports, "history flag must not return protected latest copy")
		}
	}
}
