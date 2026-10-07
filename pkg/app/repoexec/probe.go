package repoexec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Probe runs a bounded informational command with repository re-delegation
// disabled. The caller must authorize the selected executable first.
func Probe(ctx context.Context, target, cwd string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, target, args...)
	cmd.WaitDelay = time.Second
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "RZM_REPO_DELEGATED=1", "RZM_SKIP_REPO_DELEGATE=1")
	out := &boundedOutput{limit: 256 * 1024}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("selected executable did not complete %s in time: %w", strings.Join(args, " "), ctx.Err())
		}
		return "", fmt.Errorf("selected executable did not complete %s successfully: %w", strings.Join(args, " "), err)
	}
	return out.String(), nil
}

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("executable output exceeded limit")
	}
	return b.Buffer.Write(p)
}

// Version reports the exact version string used in runtime build identity.
func Version(ctx context.Context, target, cwd string) (string, error) {
	out, err := Probe(ctx, target, cwd, "--version")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	for i, field := range fields {
		if field == "version" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("selected executable did not report a Rhizome version")
}
