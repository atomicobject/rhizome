package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// DedupeTracker records which context pieces were already sent to a session.
type DedupeTracker interface {
	Allow(key, fingerprint string) bool
	MarkSent(key, fingerprint string)
	Seen(key, fingerprint string) bool
}

// DedupeItem is one materialized context candidate and its stable fingerprint.
type DedupeItem struct {
	Key         string
	Fingerprint string
}

// BatchDedupeTracker reserves all materialized candidates in one operation.
// The returned reservation owns the matching candidates until Commit releases it.
type BatchDedupeTracker interface {
	Reserve(items []DedupeItem) BatchDedupeReservation
}

// BatchDedupeReservation reports which candidates this invocation owns and
// commits only emitted candidates while releasing every omitted reservation.
type BatchDedupeReservation interface {
	Allowed(key, fingerprint string) bool
	Commit(emitted []DedupeItem)
}

// renderedNoteDedupe collects candidate note bodies retained by a renderer.
// Final assembly decides which complete containers may be marked sent.
type renderedNoteDedupe struct {
	vaultPaths paths.VaultPaths
	tracker    DedupeTracker
	items      []DedupeItem
}

func (d *renderedNoteDedupe) include(path, content string) bool {
	key := noteDedupeKey(path, d.vaultPaths)
	if key == "" {
		return true
	}
	fingerprint := pieceFingerprint(content)
	if d.tracker != nil && d.tracker.Seen(key, fingerprint) {
		return false
	}
	d.items = append(d.items, DedupeItem{Key: key, Fingerprint: fingerprint})
	return true
}

func pieceFingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// packContextOptions configures the unified pack entry point.
type packContextOptions struct {
	Context        context.Context
	Pieces         []contextpack.Piece
	Budget         int
	Tracker        DedupeTracker
	NestedDedupe   map[string][]DedupeItem
	IncompleteKeys map[string]bool
	Intent         string
	Compressor     contextpack.Compressor
}

// packContextResult holds the result of packing context pieces.
type packContextResult struct {
	Text       string
	Meta       contextpack.Meta
	Compressed bool // true if LLM compression was used
	Delivery   *contextDelivery
}

// contextDelivery carries owned reservations and renderer-proven byte ends
// through enclosing assembly. No source fingerprint is committed by packing.
type contextDelivery struct {
	groups []*contextDeliveryGroup
}

type contextDeliveryGroup struct {
	reservation BatchDedupeReservation
	tracker     DedupeTracker
	entries     []contextDeliveryEntry
	finished    bool
}

type contextDeliveryEntry struct {
	end  int
	item DedupeItem
}

func (d *contextDelivery) shift(offset int) {
	for _, group := range d.groups {
		for i := range group.entries {
			group.entries[i].end += offset
		}
	}
}

func (d *contextDelivery) clip(retainedBytes int) {
	for _, group := range d.groups {
		kept := group.entries[:0]
		for _, entry := range group.entries {
			if entry.end > 0 && entry.end <= retainedBytes {
				kept = append(kept, entry)
			}
		}
		group.entries = kept
	}
}

func (d *contextDelivery) release() {
	if d == nil {
		return
	}
	for _, group := range d.groups {
		if !group.finished && group.reservation != nil {
			group.reservation.Commit(nil)
		}
		group.finished = true
	}
}

func (d *contextDelivery) finish(ctx context.Context) error {
	if ctx != nil && ctx.Err() != nil {
		d.release()
		return ctx.Err()
	}
	if d == nil {
		return nil
	}
	for _, group := range d.groups {
		if group.finished {
			continue
		}
		items := make([]DedupeItem, 0, len(group.entries))
		for _, entry := range group.entries {
			items = append(items, entry.item)
		}
		if group.reservation != nil {
			group.reservation.Commit(items)
		} else if group.tracker != nil {
			for _, item := range items {
				group.tracker.MarkSent(item.Key, item.Fingerprint)
			}
		}
		group.finished = true
	}
	return nil
}

// packContext is the unified entry point for packing context pieces.
// It handles deduplication and optional compression in one place.
func packContext(opts packContextOptions) packContextResult {
	var pieces []contextpack.Piece
	var fingerprints map[string]string
	var reservation BatchDedupeReservation
	if tracker, ok := opts.Tracker.(BatchDedupeTracker); ok {
		fingerprints = fingerprintDedupePieces(opts.Pieces)
		keys := make([]string, len(opts.Pieces))
		for i, piece := range opts.Pieces {
			keys[i] = piece.Key
		}
		reservation = tracker.Reserve(dedupeItemsForKeys(keys, fingerprints, opts.NestedDedupe))
		pieces = filterBatchDedupedPieces(opts.Pieces, fingerprints, reservation)
	} else {
		pieces, fingerprints = filterDedupedPieces(opts.Pieces, opts.Tracker)
	}

	var result packContextResult
	if opts.Compressor != nil {
		text, metaWithComp := contextpack.PackWithIntent(pieces, opts.Budget, contextpack.PackOptions{
			Intent:         opts.Intent,
			Compressor:     opts.Compressor,
			Ctx:            opts.Context,
			AlwaysCompress: true,
		})
		result = packContextResult{
			Text:       text,
			Meta:       metaWithComp.Meta,
			Compressed: metaWithComp.Compressed,
		}
	}

	group := &contextDeliveryGroup{reservation: reservation, tracker: opts.Tracker}
	result.Delivery = &contextDelivery{groups: []*contextDeliveryGroup{group}}
	if !result.Compressed {
		var included []contextpack.Piece
		result.Text, result.Meta, included = contextpack.PackDetailed(pieces, opts.Budget)
		end := 0
		for i, piece := range included {
			if i > 0 {
				end += 2 // PackDetailed joins pieces with two newlines.
			}
			end += len(piece.Text)
			if opts.IncompleteKeys[piece.Key] || pieceFingerprint(piece.Text) != fingerprints[piece.Key] {
				continue
			}
			for _, item := range dedupeItemsForKeys([]string{piece.Key}, fingerprints, opts.NestedDedupe) {
				group.entries = append(group.entries, contextDeliveryEntry{end: end, item: item})
			}
		}
	}
	// Compressor status is not exact raw-body evidence. Successful compression
	// leaves original containers and nested bodies eligible for a later read.
	return result
}

func fingerprintDedupePieces(pieces []contextpack.Piece) map[string]string {
	fingerprints := make(map[string]string, len(pieces))
	for _, piece := range pieces {
		if text := strings.TrimSpace(piece.Text); text != "" {
			fingerprints[piece.Key] = pieceFingerprint(text)
		}
	}
	return fingerprints
}

func dedupeItemsForKeys(keys []string, fingerprints map[string]string, nested map[string][]DedupeItem) []DedupeItem {
	items := make([]DedupeItem, 0, len(keys)+len(nested))
	seen := make(map[string]struct{}, len(keys)+len(nested))
	appendItem := func(key, fingerprint string) {
		if key == "" || fingerprint == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		items = append(items, DedupeItem{Key: key, Fingerprint: fingerprint})
	}
	for _, key := range keys {
		appendItem(key, fingerprints[key])
		for _, item := range nested[key] {
			appendItem(item.Key, item.Fingerprint)
		}
	}
	return items
}

func filterBatchDedupedPieces(pieces []contextpack.Piece, fingerprints map[string]string, reservation BatchDedupeReservation) []contextpack.Piece {
	if reservation == nil {
		return pieces
	}
	filtered := make([]contextpack.Piece, 0, len(pieces))
	for _, piece := range pieces {
		fingerprint := fingerprints[piece.Key]
		if fingerprint == "" || reservation.Allowed(piece.Key, fingerprint) {
			filtered = append(filtered, piece)
		}
	}
	return filtered
}

// filterDedupedPieces removes pieces already seen by the tracker and returns
// fingerprints for the remaining pieces.
func filterDedupedPieces(pieces []contextpack.Piece, tracker DedupeTracker) ([]contextpack.Piece, map[string]string) {
	if tracker == nil || len(pieces) == 0 {
		return pieces, nil
	}

	filtered := make([]contextpack.Piece, 0, len(pieces))
	fingerprints := make(map[string]string, len(pieces))

	for _, p := range pieces {
		txt := strings.TrimSpace(p.Text)
		if txt == "" {
			filtered = append(filtered, p)
			continue
		}
		fp := pieceFingerprint(txt)
		if tracker.Seen(p.Key, fp) {
			// Already seen - skip this piece
			continue
		}
		fingerprints[p.Key] = fp
		filtered = append(filtered, p)
	}

	return filtered, fingerprints
}
