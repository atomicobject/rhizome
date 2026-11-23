//go:build !windows

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// runDueWithin runs one pass and fails the test if it does not finish in
// time, as when a run blocks opening a FIFO.
func runDueWithin(t *testing.T, svc *Service, d time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := svc.RunDue(context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(d):
		t.Fatal("the pass hung on a file the agent left")
	}
}

// The agent controls its run folder, so the result file is read only when
// it is a regular file in the runner's real folder: a FIFO cannot hang the
// run, and a symlink cannot make Rhizome read another file.
func TestResultFileMustBeARegularFileInTheRunFolder(t *testing.T) {
	for name, plant := range map[string]func(t *testing.T, result, outside string){
		"fifo": func(t *testing.T, result, _ string) {
			require.NoError(t, syscall.Mkfifo(result, 0o600))
		},
		"symlink": func(t *testing.T, result, outside string) {
			require.NoError(t, os.Symlink(filepath.Join(outside, resultFile), result))
		},
		"symlinked run folder": func(t *testing.T, result, outside string) {
			require.NoError(t, os.RemoveAll(filepath.Dir(result)))
			require.NoError(t, os.Symlink(outside, filepath.Dir(result)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, resultFile), `{"status": "attention", "summary": "read from outside the run"}`)
			agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
				plant(t, envValue(req.Env, "RZM_TASK_RESULT_FILE"), outside)
				return AgentResult{Message: "done"}, nil
			}}
			f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, agent, "")
			runDueWithin(t, f.svc, 20*time.Second)

			r := lastRun(t, f.svc, "a")
			require.NotNil(t, r.Result)
			require.Empty(t, r.Result.Status, "a refused file counts as ok")
			require.NotEmpty(t, r.Result.Error)
			require.Nil(t, activeItem(t, f.svc, "a"))
			require.Empty(t, f.notifications(t))
		})
	}
}

// Trimming the handoff note never writes through a symlink or blocks on a
// FIFO the agent put in its place.
func TestHandoffTrimLeavesLinksAndFIFOsAlone(t *testing.T) {
	long := strings.Repeat("a line the next run may need\n", 1000)
	for name, plant := range map[string]func(t *testing.T, handoff, target string){
		"fifo": func(t *testing.T, handoff, _ string) {
			require.NoError(t, syscall.Mkfifo(handoff, 0o600))
		},
		"symlink": func(t *testing.T, handoff, target string) {
			require.NoError(t, os.Symlink(target, handoff))
		},
	} {
		t.Run(name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "keep.md")
			writeFile(t, target, long)
			agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
				plant(t, filepath.Join(envValue(req.Env, "RZM_TASK_DATA_DIR"), handoffFile), target)
				return AgentResult{Message: "done"}, nil
			}}
			f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nmemory: true\nprompt: p\n"}, agent, "")
			runDueWithin(t, f.svc, 20*time.Second)

			data, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, long, string(data), "the link's target is untouched")
		})
	}
}

// notify.log is written as a new file in the run folder, never through a
// link the agent left there.
func TestNotifyLogIsNotWrittenThroughALink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "keep.txt")
	writeFile(t, target, "keep\n")
	agent := &fakeAgent{run: func(req AgentRequest) (AgentResult, error) {
		result := envValue(req.Env, "RZM_TASK_RESULT_FILE")
		require.NoError(t, os.Symlink(target, filepath.Join(filepath.Dir(result), notifyLogFile)))
		writeResult(t, req, `{"status": "attention", "summary": "look"}`)
		return AgentResult{Message: "done"}, nil
	}}
	f := newNotifyFixture(t, map[string]string{"a.yml": "name: A\nevery: 1h\nprompt: p\n"}, agent, "echo sent >&2")
	_, err := f.svc.RunDue(context.Background())
	require.NoError(t, err)

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "keep\n", string(data))
	log := f.svc.runFile(lastRun(t, f.svc, "a").ID, notifyLogFile)
	info, err := os.Lstat(log)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular())
	data, err = os.ReadFile(log)
	require.NoError(t, err)
	require.Equal(t, "sent\n", string(data))
}
