package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/atomicobject/rhizome/pkg/harness/internal/processlog"
)

type Spec struct {
	Path  string
	Args  []string
	Dir   string
	Stdin io.Reader
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type OSRunner struct {
	Label string
}

func (OSRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (r OSRunner) Run(ctx context.Context, spec Spec) (Result, error) {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	var interrupted atomic.Bool
	cancelCommand := cmd.Cancel
	cmd.Cancel = func() error {
		err := cancelCommand()
		if err == nil {
			interrupted.Store(true)
		}
		return err
	}
	cmd.Dir = spec.Dir
	cmd.Stdin = spec.Stdin
	var stdout bytes.Buffer
	var stderr tailBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	corr := processlog.CorrelationID()
	if err := cmd.Start(); err != nil {
		return Result{}, err
	}
	processlog.Spawn(r.Label, spec.Path, spec.Dir, corr, cmd.Process.Pid)
	err := cmd.Wait()
	processlog.Exit(r.Label, corr, cmd.Process.Pid, cmd.ProcessState, err)
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		// Wait prefers the child's exit error even when context cancellation killed it.
		if interrupted.Load() {
			return result, ctx.Err()
		}
		return result, nil
	}
	return result, err
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	const limit = 16 * 1024
	if len(b.data) > limit {
		b.data = b.data[len(b.data)-limit:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
