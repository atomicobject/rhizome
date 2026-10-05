package semantic

import "time"

const (
	defaultCodeEmbedPackerMaxTexts = 256
	defaultCodeEmbedPackerMaxBytes = 192 << 10
	defaultCodeEmbedPackerMinTexts = 128
	defaultCodeEmbedPackerMaxWait  = 200 * time.Millisecond
)

// FullScanAdaptivePackerFloor is the default adaptive dispatch floor for full scans.
const FullScanAdaptivePackerFloor = 512

// EmbedPackerOptions controls staged embedding request packing.
type EmbedPackerOptions struct {
	// MaxTexts is the hard text-count ceiling for one provider request.
	MaxTexts int
	// MaxBytes is the hard approximate byte ceiling for one provider request.
	MaxBytes int
	// MinTexts is the normal dispatch floor while slots are available.
	MinTexts int
	// AdaptiveMinTexts is the larger floor used after warmup on full-scan lanes.
	AdaptiveMinTexts int
	// MaxWait bounds latency when a batch cannot reach MinTexts.
	MaxWait time.Duration
}

func normalizeEmbedPackerOptions(opts EmbedPackerOptions) EmbedPackerOptions {
	if opts.MaxTexts <= 0 {
		opts.MaxTexts = defaultCodeEmbedPackerMaxTexts
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = defaultCodeEmbedPackerMaxBytes
	}
	if opts.MinTexts <= 0 {
		opts.MinTexts = defaultCodeEmbedPackerMinTexts
	}
	if opts.MinTexts > opts.MaxTexts {
		opts.MinTexts = opts.MaxTexts
	}
	if opts.AdaptiveMinTexts < 0 {
		opts.AdaptiveMinTexts = 0
	}
	if opts.MaxWait <= 0 {
		opts.MaxWait = defaultCodeEmbedPackerMaxWait
	}
	return opts
}
