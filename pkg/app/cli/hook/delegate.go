package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Delegate runs `target hook <harness> <point>`, the repository's selected
// rzm, as a supervised child under opts.Deadline, with env as its
// environment. The child reads the host's payload from opts.Stdin. Its
// output reaches opts.Stdout only when it exits 0 within the deadline and
// prints well-formed JSON within the size bound. A version marker selects
// the binary but does not prove it handles hooks, so a nonzero exit, a
// timeout, or a binary that does not start ends with no output and a
// diagnostic, and a timeout kills the child's process tree.
//
// Docs: [[agent-surface-integration-modes#^SPEC-0045-US5-AC7]]
func Delegate(ctx context.Context, target string, env []string, opts Options) {
	d := newDispatch(ctx, opts)
	defer d.cancel()
	defer func() {
		if p := recover(); p != nil {
			d.log("panic: %v", p)
		}
	}()
	if err := d.delegate(target, env); err != nil {
		d.log("hand off to %s: %v", target, err)
	}
}

func (d *dispatch) delegate(target string, env []string) error {
	cmd := exec.CommandContext(d.ctx, target, "hook", d.opts.Harness, d.opts.Point)
	cmd.Dir = d.opts.Cwd
	cmd.Env = env
	cmd.Stdin = d.opts.Stdin
	stdout := &boundedBuffer{limit: maxHandlerOutputBytes}
	stderr := &boundedBuffer{limit: 2000}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	configureProcessTree(cmd)
	cmd.Cancel = func() error { return killProcessTree(cmd) }
	cmd.WaitDelay = 100 * time.Millisecond
	runErr := cmd.Run()
	errText := strings.TrimSpace(stderr.String())
	switch {
	case d.ctx.Err() != nil:
		return fmt.Errorf("timed out at the deadline; stderr: %s", errText)
	case runErr != nil:
		return fmt.Errorf("%v; stderr: %s", runErr, errText)
	case stdout.overflow:
		return errors.New("output exceeds 256 KiB")
	}
	var errs []error
	if errText != "" {
		errs = append(errs, fmt.Errorf("stderr: %s", errText))
	}
	if out := bytes.TrimSpace(stdout.Bytes()); len(out) > 0 {
		if !json.Valid(out) {
			return errors.Join(append(errs, errors.New("output is not JSON"))...)
		}
		if _, err := d.opts.Stdout.Write(append(out, '\n')); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Diagnose records one diagnostic for an invocation that ends before it
// dispatches, such as one whose inherited deadline has already passed.
func Diagnose(opts Options, format string, args ...any) {
	d := newDispatch(context.Background(), opts)
	defer d.cancel()
	d.log(format, args...)
}
