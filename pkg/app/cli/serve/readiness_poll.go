// Package serve coordinates long-running server application behavior below
// the Cobra command adapter.
package serve

import (
	"context"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/web"
)

func markUsableNoteModel(ctx context.Context, runtime *web.Runtime, count modelCountFunc) bool {
	if runtime == nil || count == nil {
		return false
	}
	countValue, err := count(ctx)
	if err != nil || countValue == 0 {
		return false
	}
	runtime.MarkNoteReadReady()
	return true
}

// waitForUsableNoteModel opens the note-read gate as soon as durable exact-note
// rows exist, backing off so a cold vault does not repeat a full scan every
// quarter second.
func waitForUsableNoteModel(ctx context.Context, runtime *web.Runtime, interval time.Duration, count modelCountFunc) bool {
	if runtime == nil || count == nil {
		return false
	}
	if interval <= 0 {
		interval = readinessPollInterval
	}
	if interval > noteReadinessMaxPollInterval {
		interval = noteReadinessMaxPollInterval
	}
	pollInterval := interval
	for {
		if runtime.NoteReadReady() {
			return true
		}
		countValue, err := count(ctx)
		if err == nil && countValue > 0 {
			runtime.MarkNoteReadReady()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollInterval):
		}
		pollInterval = nextNoteReadinessPollInterval(pollInterval)
	}
}

func nextNoteReadinessPollInterval(current time.Duration) time.Duration {
	if current >= noteReadinessMaxPollInterval {
		return current
	}
	next := current * 2
	if next > noteReadinessMaxPollInterval || next < current {
		return noteReadinessMaxPollInterval
	}
	return next
}

// waitForUsableIndexModel opens the index gate once a complete ontology model
// is readable, whoever produced it.
func waitForUsableIndexModel(ctx context.Context, runtime *web.Runtime, interval time.Duration, count modelCountFunc) bool {
	if runtime == nil || count == nil {
		return false
	}
	if interval <= 0 {
		interval = readinessPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if runtime.IndexReady() {
			return true
		}
		n, err := count(ctx)
		if err == nil && n > 0 {
			runtime.MarkIndexReady()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}
