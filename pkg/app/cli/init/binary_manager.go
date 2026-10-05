package init

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

func normalizeBinaryManagerOption(opts RunOptions) (string, error) {
	manager := strings.TrimSpace(opts.BinaryManager)
	if manager == "" {
		return "", nil
	}
	if manager != obsidian.BinaryManagerExternal {
		return "", fmt.Errorf("--binary-manager supports only %q", obsidian.BinaryManagerExternal)
	}
	return manager, nil
}

func hasBinaryManagerOption(opts RunOptions) bool {
	return strings.TrimSpace(opts.BinaryManager) != ""
}

// applyBinaryManagerOption hands the Rhizome binary to an external manager
// when --binary-manager asks for it. The change list shows the switch, so the
// rerun confirmation covers it.
func applyBinaryManagerOption(cfg *obsidian.LocalConfig, opts RunOptions) (bool, error) {
	if cfg == nil || !hasBinaryManagerOption(opts) {
		return false, nil
	}
	externallyManaged, err := cfg.Rhizome.UsesExternalBinaryManager()
	if err != nil {
		return false, err
	}
	if externallyManaged {
		return false, nil
	}

	// Existing .rhizome/bin contents are a cache. Ownership migration changes
	// only authored config and deliberately leaves that cache recoverable.
	cfg.Rhizome.BinaryManager = opts.BinaryManager
	cfg.Rhizome.Version = ""
	cfg.Rhizome.DevBinaryDir = ""
	cfg.Rhizome.BinaryDir = ""
	cfg.Rhizome.BinaryPath = ""
	return true, nil
}
