package hook

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Delegate runs `target hook <harness> <point>`, the repository's selected
// rzm, as a supervised child under opts.Deadline, with env as its
// environment. The child reads the host's payload from opts.Stdin and
// writes straight to opts.Stdout, the host's own output when that is a
// file: it emits its items and only then records them, so the binary that
// emits owns the commit. A version marker selects the binary but does not
// prove it handles hooks, so a nonzero exit, a timeout, or a binary that
// does not start leaves a diagnostic, a timeout kills the child's process
// tree, and the invocation still exits 0. A child killed after writing but
// before recording may deliver those items again later; one killed while
// writing leaves truncated output the host rejects, with nothing recorded.
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
	cmd.Stdout = d.opts.Stdout
	stderr := &boundedBuffer{limit: 2000}
	cmd.Stderr = stderr
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
	case errText != "":
		return fmt.Errorf("stderr: %s", errText)
	}
	return nil
}

// Diagnose records one diagnostic for an invocation that ends before it
// dispatches, such as one whose inherited deadline has already passed.
func Diagnose(opts Options, format string, args ...any) {
	d := newDispatch(context.Background(), opts)
	defer d.cancel()
	d.log(format, args...)
}
