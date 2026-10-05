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

func (d *Driver) checkedVersion(ctx context.Context, path string) (string, error) {
	d.versionMu.Lock()
	defer d.versionMu.Unlock()
	if !d.versionAt.IsZero() && time.Since(d.versionAt) < versionCacheTTL {
		return d.versionValue, d.versionErr
	}
	result, err := d.runner.Run(ctx, command.Spec{Path: path, Args: []string{"--version"}})
	version, checkErr := codexVersionResult(ctx, result, err)
	if !errors.Is(checkErr, harness.ErrTimeout) {
		d.versionAt, d.versionValue, d.versionErr = time.Now(), version, checkErr
	}
	return version, checkErr
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
