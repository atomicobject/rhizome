package codeanchor

import (
	"os"
	"runtime"
	"time"
)

// TreeSitterLimits configures parse timeouts.
type TreeSitterLimits struct {
	ParseTimeout time.Duration
}

const (
	treeSitterEnvTimeout  = "RHIZOME_TS_PARSE_TIMEOUT"
	treeSitterBaseTimeout = 10 * time.Second
	treeSitterMaxTimeout  = 60 * time.Second
	treeSitterPerKBTO     = 5 * time.Millisecond
)

func resolveTreeSitterLimits(content []byte, cfg TreeSitterLimits) time.Duration {
	timeout := cfg.ParseTimeout
	if raw := os.Getenv(treeSitterEnvTimeout); raw != "" {
		if val, err := time.ParseDuration(raw); err == nil {
			timeout = val
		}
	}

	sizeKB := len(content) / 1024
	if timeout == 0 {
		base := treeSitterBaseTimeout
		maxTO := treeSitterMaxTimeout
		if runtime.GOOS == "windows" {
			base += 5 * time.Second
			maxTO += 10 * time.Second
		}
		to := base + time.Duration(sizeKB)*treeSitterPerKBTO
		if to > maxTO {
			to = maxTO
		}
		timeout = to
	}

	return timeout
}
