package init

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	appupdate "github.com/atomicobject/rhizome/pkg/app/update"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// installPinnedBinary is the self-healing install seam. It is a var so tests
// can stub the install without real HTTP.
var installPinnedBinary = appupdate.EnsurePinnedBinary

// ensurePinnedBinary makes the repo-local pinned binary current using the same
// marker check, install lock, checksum, and smoke-test path the global binary's
// delegation bootstrap uses (pkg/app/update.EnsurePinnedBinary). It skips
// silently when the marker already matches the pin, and only prints a notice
// when an install will actually happen.
func ensurePinnedBinary(root string, cfg obsidian.LocalConfig, opts RunOptions) error {
	externallyManaged, err := cfg.Rhizome.UsesExternalBinaryManager()
	if err != nil {
		return err
	}
	if externallyManaged {
		return nil
	}
	pin := appupdate.NormalizeVersion(cfg.Rhizome.Version)
	if pin == "" || pin == "latest" {
		return nil
	}

	target := pinnedBinaryPath(root)
	if marker, ok := appupdate.ReadVersionMarker(target); ok && marker == pin {
		return nil
	}

	stdout := opts.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	fmt.Fprintf(stdout, "\nInstalling Rhizome %s for this repo (%s)\n", pin, pinnedInstallReason(target, pin))

	cfgCopy := cfg
	if err := installPinnedBinary(context.Background(), appupdate.EnsureOptions{
		CfgDir:   root,
		Config:   &cfgCopy,
		Progress: stdout,
	}); err != nil {
		return fmt.Errorf("install pinned Rhizome binary: %w", err)
	}
	return nil
}

// pinnedInstallReason describes why the repo-local binary needs installing, for
// the one-line notice. It mirrors the marker-based decision EnsurePinnedBinary
// makes internally.
func pinnedInstallReason(target, pin string) string {
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return "repo-local binary missing"
	}
	marker, ok := appupdate.ReadVersionMarker(target)
	if !ok {
		return "repo-local binary version unknown"
	}
	if marker != pin {
		return fmt.Sprintf("repo-local binary is %s, expected %s", marker, pin)
	}
	return "refreshing pinned binary"
}

func pinnedBinaryPath(root string) string {
	return filepath.Join(root, ".rhizome", "bin", appupdate.CurrentPlatformDir(), pinnedExecutableName())
}

func pinnedExecutableName() string {
	if runtime.GOOS == "windows" {
		return "rzm.exe"
	}
	return "rzm"
}
