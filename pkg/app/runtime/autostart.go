package runtime

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AutostartEnv disables auto-start for one invocation when set to 0/false/no.
const AutostartEnv = "RZM_RUNTIME_AUTOSTART"

// AutostartEnabled resolves whether clients may spawn a headless runtime:
// the environment override wins, then `runtime.autostart` in the local
// config, then true.
func AutostartEnabled(cfg *obsidian.LocalConfig) bool {
	if value, ok := os.LookupEnv(AutostartEnv); ok {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "0", "false", "no", "off":
			return false
		case "1", "true", "yes", "on":
			return true
		}
	}
	if cfg != nil && cfg.Runtime != nil && cfg.Runtime.Autostart != nil {
		return *cfg.Runtime.Autostart
	}
	return true
}

// IdleTimeout resolves the headless idle exit duration from local config.
func IdleTimeout(cfg *obsidian.LocalConfig) (time.Duration, error) {
	if cfg == nil || cfg.Runtime == nil || strings.TrimSpace(cfg.Runtime.IdleTimeout) == "" {
		return DefaultIdleTimeout, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(cfg.Runtime.IdleTimeout))
	if err != nil {
		return 0, fmt.Errorf("runtime.idleTimeout: %w", err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("runtime.idleTimeout must be positive, got %s", cfg.Runtime.IdleTimeout)
	}
	return d, nil
}
