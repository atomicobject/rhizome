package semantic

import "time"

type adaptiveDispatchInput struct {
	Opts          EmbedPackerOptions
	Pending       int
	PendingBytes  int
	Active        int
	MaxConcurrent int
	Warmups       int
	OldestAge     time.Duration
	Draining      bool
}

type adaptiveDispatchDecision struct {
	Ready      bool
	Threshold  int
	BatchTexts int
	Warmup     bool
}

func adaptiveDispatch(in adaptiveDispatchInput) adaptiveDispatchDecision {
	opts := normalizeEmbedPackerOptions(in.Opts)
	if in.Pending <= 0 {
		return adaptiveDispatchDecision{Threshold: opts.MinTexts, BatchTexts: opts.MaxTexts}
	}
	if in.Draining {
		return adaptiveDispatchDecision{Ready: true, Threshold: 1, BatchTexts: opts.MaxTexts}
	}

	floor := adaptiveFloorFor(opts)
	if !adaptiveModeFor(opts, floor) {
		return adaptiveDispatchDecision{Ready: in.Pending >= opts.MinTexts, Threshold: opts.MinTexts, BatchTexts: opts.MaxTexts}
	}

	availableSlots := in.MaxConcurrent - in.Active
	if availableSlots < 1 {
		availableSlots = 1
	}
	target := ceilDiv(in.Pending, availableSlots)
	if target < floor {
		target = floor
	}
	if target > opts.MaxTexts {
		target = opts.MaxTexts
	}

	threshold := target
	batchTexts := target
	warmup := in.Warmups < adaptiveWarmupBudget(in.MaxConcurrent)
	if warmup {
		threshold = floor
		batchTexts = floor
	}

	if opts.MaxBytes > 0 && in.PendingBytes >= opts.MaxBytes {
		threshold = min(threshold, in.Pending)
	}
	if in.Active == 0 && in.Pending >= floor && in.Pending < opts.MaxTexts {
		threshold = min(threshold, floor)
		batchTexts = min(batchTexts, floor)
	}
	if opts.MaxWait > 0 && in.OldestAge >= opts.MaxWait/2 {
		threshold = min(threshold, max(floor, opts.MaxTexts/2))
		batchTexts = min(batchTexts, max(floor, opts.MaxTexts/2))
	}
	if opts.MaxWait > 0 && in.OldestAge >= opts.MaxWait {
		threshold = 1
		batchTexts = min(batchTexts, max(1, in.Pending))
	}

	if threshold < 1 {
		threshold = 1
	}
	if batchTexts < 1 {
		batchTexts = 1
	}
	return adaptiveDispatchDecision{
		Ready:      in.Pending >= threshold,
		Threshold:  threshold,
		BatchTexts: batchTexts,
		Warmup:     warmup && threshold <= floor,
	}
}

func adaptiveFloorFor(opts EmbedPackerOptions) int {
	floor := opts.AdaptiveMinTexts
	if floor <= 0 {
		floor = FullScanAdaptivePackerFloor
	}
	if floor < 1 {
		floor = 1
	}
	if opts.MaxTexts > 0 && floor > opts.MaxTexts {
		floor = opts.MaxTexts
	}
	return floor
}

func adaptiveModeFor(opts EmbedPackerOptions, floor int) bool {
	return floor > 0 && opts.MaxTexts > floor && opts.MinTexts <= floor
}

func adaptiveWarmupBudget(maxConcurrent int) int {
	if maxConcurrent < 1 {
		return 1
	}
	slots := maxConcurrent / 8
	if slots < 4 {
		slots = 4
	}
	if slots > 8 {
		slots = 8
	}
	if slots > maxConcurrent {
		return maxConcurrent
	}
	return slots
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return a
	}
	if a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}
