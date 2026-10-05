package diagnostics

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultRetentionDays         = 7
	DefaultMaxBytes        int64 = 64 << 20
	DefaultMaxSegmentBytes int64 = 4 << 20
	DefaultMaxEventBytes         = 32 << 10
	DefaultMaxReportBytes        = 1 << 20
)

// Config is the repository diagnostics subtree, also embedded by strict config
// readers so settings survive ordinary repository configuration updates.
type Config struct {
	Enabled       *bool  `yaml:"enabled,omitempty"`
	RetentionDays *int   `yaml:"retentionDays,omitempty"`
	MaxBytes      *int64 `yaml:"maxBytes,omitempty"`
	Level         string `yaml:"level,omitempty"`
}

// Process output capture can replace the os.Stderr variable during a runtime
// handoff. Cache its initial writer so default construction never races with
// that reassignment; captured hosts pass their owned writer explicitly.
var defaultStderr io.Writer = os.Stderr

// LoadOptions decodes only the diagnostics section of repository configuration.
// It does not load user configuration or initialize indexing or a runtime.
func LoadOptions(vaultRoot string) (Options, error) {
	opts := Options{}
	f, err := os.Open(filepath.Join(vaultRoot, ".rhizome", "config.yml"))
	if os.IsNotExist(err) {
		return defaults(opts), nil
	}
	if err != nil {
		return defaults(opts), fmt.Errorf("open diagnostics configuration: %w", err)
	}
	defer f.Close()
	const maxConfigBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil {
		return defaults(opts), fmt.Errorf("read diagnostics configuration: %w", err)
	}
	if len(data) > maxConfigBytes {
		return defaults(opts), fmt.Errorf("diagnostics configuration exceeds %d bytes", maxConfigBytes)
	}
	var root struct {
		Diagnostics yaml.Node `yaml:"diagnostics"`
	}
	if err := yaml.Unmarshal(data, &root); err != nil {
		// YAML errors can quote unrelated configuration values.
		return defaults(opts), fmt.Errorf("invalid repository YAML while loading diagnostics configuration")
	}
	var cfg Config
	if root.Diagnostics.Kind != 0 {
		// Decode the opt-out separately so another invalid setting cannot
		// re-enable persistence while reporting its configuration error.
		var enabled struct {
			Enabled *bool `yaml:"enabled"`
		}
		if err := root.Diagnostics.Decode(&enabled); err == nil && enabled.Enabled != nil {
			opts.Disabled = !*enabled.Enabled
		}
		if err := root.Diagnostics.Decode(&cfg); err != nil {
			return defaults(opts), fmt.Errorf("invalid diagnostics configuration")
		}
	}
	if cfg.Enabled != nil {
		opts.Disabled = !*cfg.Enabled
	}
	if cfg.RetentionDays != nil {
		if *cfg.RetentionDays <= 0 {
			return defaults(opts), fmt.Errorf("diagnostics.retentionDays must be positive")
		}
		opts.RetentionDays = *cfg.RetentionDays
	}
	if cfg.MaxBytes != nil {
		if *cfg.MaxBytes < 4096 {
			return defaults(opts), fmt.Errorf("diagnostics.maxBytes must be at least 4096")
		}
		opts.MaxBytes = *cfg.MaxBytes
	}
	if cfg.Level != "" {
		switch strings.ToLower(cfg.Level) {
		case "debug", "info", "warn", "error":
			_ = opts.Level.UnmarshalText([]byte(cfg.Level))
		default:
			return defaults(opts), fmt.Errorf("diagnostics.level must be debug, info, warn, or error")
		}
	}
	return defaults(opts), nil
}

func defaults(opts Options) Options {
	if opts.RetentionDays <= 0 {
		opts.RetentionDays = DefaultRetentionDays
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.MaxSegmentBytes <= 0 {
		opts.MaxSegmentBytes = DefaultMaxSegmentBytes
	}
	if opts.MaxSegmentBytes > opts.MaxBytes/4 {
		opts.MaxSegmentBytes = opts.MaxBytes / 4
	}
	if opts.MaxSegmentBytes < 1 {
		opts.MaxSegmentBytes = 1
	}
	if opts.MaxEventBytes <= 0 {
		opts.MaxEventBytes = DefaultMaxEventBytes
	}
	if int64(opts.MaxEventBytes) > opts.MaxSegmentBytes {
		opts.MaxEventBytes = int(opts.MaxSegmentBytes)
	}
	if opts.MaxReportBytes <= 0 {
		opts.MaxReportBytes = DefaultMaxReportBytes
	}
	if int64(opts.MaxReportBytes) > opts.MaxBytes/4 {
		opts.MaxReportBytes = int(opts.MaxBytes / 4)
	}
	if opts.Stderr == nil {
		opts.Stderr = defaultStderr
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	opts.Role, _ = boundedString(opts.Role, 128)
	opts.Version, _ = boundedString(opts.Version, 128)
	return opts
}
