package codex

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/command"
)

const versionCacheTTL = 10 * time.Minute

var versionPattern = regexp.MustCompile(`\b(\d+)\.(\d+)\.(\d+)\b`)

// versionProbe is one in-flight --version run; done closes once version and
// err are set.
type versionProbe struct {
	done    chan struct{}
	version string
	err     error
}

// checkedVersion runs at most one probe at a time, outside versionMu, so a
// caller waiting on another caller's probe still honors its own ctx.
func (d *Driver) checkedVersion(ctx context.Context, path string) (string, error) {
	for {
		d.versionMu.Lock()
		if !d.versionAt.IsZero() && time.Since(d.versionAt) < versionCacheTTL {
			version, err := d.versionValue, d.versionErr
			d.versionMu.Unlock()
			return version, err
		}
		probe := d.versionProbe
		if probe == nil {
			probe = &versionProbe{done: make(chan struct{})}
			d.versionProbe = probe
			d.versionMu.Unlock()
			result, err := d.runner.Run(ctx, command.Spec{Path: path, Args: []string{"--version"}})
			probe.version, probe.err = codexVersionResult(ctx, result, err)
			d.versionMu.Lock()
			d.versionProbe = nil
			if !errors.Is(probe.err, harness.ErrTimeout) {
				d.versionAt, d.versionValue, d.versionErr = time.Now(), probe.version, probe.err
			}
			d.versionMu.Unlock()
			close(probe.done)
			return probe.version, probe.err
		}
		d.versionMu.Unlock()
		select {
		case <-probe.done:
			// The leader's own deadline is not this caller's; probe again.
			if !errors.Is(probe.err, harness.ErrTimeout) {
				return probe.version, probe.err
			}
		case <-ctx.Done():
			return "", harness.Phase(harness.ErrTimeout, ctx.Err())
		}
	}
}

func codexVersionResult(ctx context.Context, result command.Result, err error) (string, error) {
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", harness.Phase(harness.ErrTimeout, ctx.Err())
		}
		if isNotFound(err) {
			return "", harness.Phase(harness.ErrNotInstalled, err)
		}
		return "", harness.Phase(harness.ErrSpawn, err)
	}
	if result.ExitCode != 0 {
		return "", harness.Phase(harness.ErrCommandFailed, fmt.Errorf("exit %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr)))
	}
	match := versionPattern.FindStringSubmatch(result.Stdout)
	if match == nil {
		return "", harness.Phase(harness.ErrDecode, fmt.Errorf("invalid codex version %q", strings.TrimSpace(result.Stdout)))
	}
	version := strings.Join(match[1:], ".")
	return version, checkMinimumVersion(version)
}

func checkMinimumVersion(version string) error {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return harness.Phase(harness.ErrDecode, fmt.Errorf("invalid codex version %q", version))
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	if major == 0 && minor < 150 {
		return harness.Phase(harness.ErrInitialize, fmt.Errorf("codex %s is older than minimum 0.150.0", version))
	}
	return nil
}
