package coderefs

// Docs:
// - [Coderefs (Hub)](docs/hubs/Coderefs (Hub).md)

import "github.com/atomicobject/rhizome/pkg/vault/codepatterns"

// Config holds the runtime configuration for code reference scanning.
//
// Note: Code reference scanning uses its own independent ignore set (Includes/Excludes).
// It does NOT inherit from .obsidianignore or vault-level excludes. This is intentional:
// code refs may need to scan directories excluded from note discovery, or vice versa.
// Code ref patterns are relative to vault root and must not escape it via "..".
type Config struct {
	Enabled  bool
	Includes []string // Glob patterns for files to scan (relative to vault root)
	Excludes []string // Glob patterns for files/dirs to skip
}

// DefaultIncludes are the default glob patterns for source code files.
var DefaultIncludes = codepatterns.DefaultScanGlobs()

// DefaultExcludes are the default glob patterns to skip.
var DefaultExcludes = codepatterns.DefaultIgnoreGlobs()

// NewConfig creates a Config from includes/excludes, applying defaults if needed.
// Returns nil if enabled is false.
func NewConfig(enabled bool, includes, excludes []string) *Config {
	if !enabled {
		return nil
	}

	cfg := &Config{
		Enabled:  true,
		Includes: includes,
		Excludes: excludes,
	}

	if len(cfg.Includes) == 0 {
		cfg.Includes = DefaultIncludes
	}
	if len(cfg.Excludes) == 0 {
		cfg.Excludes = DefaultExcludes
	}

	return cfg
}
