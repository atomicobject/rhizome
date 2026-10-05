package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/cache"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ErrWatcherPending reports that the runtime cannot yet show that its read
// models reflect every note change completed before the call.
var ErrWatcherPending = errors.New("vault runtime has unapplied filesystem changes")

// SyncWatcher returns once every selected note change completed before the
// call is applied to the persisted metadata rows the read models are built
// from (SPEC-0115). It compares sources on disk with those rows, so a change
// whose filesystem event has not reached the watcher still counts, and it
// treats received watcher input the comparison cannot vouch for as waiting.
// Waiting work goes to one ownership batch that starts after the call;
// anything still waiting afterward, or an
// index rebuild in progress, returns ErrWatcherPending. Schema, configuration,
// and ontology currency are the caller's checks.
func (rt *LiveRuntime) SyncWatcher(ctx context.Context) error {
	w := rt.liveWatcher.Load()
	if w == nil || w.lane == nil {
		return ErrWatcherPending
	}
	// A rebuild can republish rows without any source change and holds the
	// lane for its whole run; the caller's own refresh is no slower.
	if status := w.lane.Status(); status.Busy && (status.JobKind == lane.KindBootCatchUp || status.JobKind == lane.KindExplicitIndex) {
		return ErrWatcherPending
	}
	changed, waiting, err := rt.sourceChanges(ctx, w)
	if err != nil || (len(changed) == 0 && !waiting) {
		return err
	}
	w.prepareSync(changed)
	handle := w.processOwnershipBatch()
	if handle == nil {
		return ErrWatcherPending
	}
	select {
	case <-handle.Done():
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := handle.Err(); err != nil {
		return err
	}
	if changed, waiting, err = rt.sourceChanges(ctx, w); err != nil {
		return err
	}
	if len(changed) > 0 || waiting {
		return ErrWatcherPending
	}
	return nil
}

// sourceChanges reports selected notes created, removed, or rewritten since
// their persisted metadata rows were captured. A size or modification-second
// difference decides without reading. Otherwise the note's content hash is
// compared once per exact stat (nanosecond time and size), so a rewrite that
// keeps the size and second is caught whenever it happened, while unchanged
// notes cost only a stat after the first check. waiting reports work only a
// batch can settle: a pending resync or configuration, ignore, or schema
// change, and a stored note that discovery no longer returns while its file
// remains.
func (rt *LiveRuntime) sourceChanges(ctx context.Context, w *unifiedSemanticWatcher) (map[string]cache.DirtyKind, bool, error) {
	store := rt.IntelStore()
	if store == nil {
		return nil, false, ErrWatcherPending
	}
	policy, err := rt.noteSelectionPolicy()
	if err != nil {
		return nil, false, err
	}
	vaultDef := rt.currentOwnershipVaultDefinition()
	discovered, err := obsidian.DiscoverFiles(vaultDef)
	if err != nil {
		return nil, false, err
	}
	rows, err := store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return nil, false, err
	}
	stored := make(map[string]semdb.NoteMetadataRow, len(rows))
	for _, row := range rows {
		stored[row.Path] = row
	}
	abs := func(rel string) string { return filepath.Join(vaultDef.BasePath(), filepath.FromSlash(rel)) }
	changed := map[string]cache.DirtyKind{}
	pending, resync := w.waitingInput()
	unseen := make(map[string]struct{}, len(rows))
	selected := make(map[string]struct{}, len(discovered))
	for path := range stored {
		unseen[path] = struct{}{}
	}
	for _, raw := range discovered {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		notePath, err := paths.CleanNotePath(raw)
		if err != nil {
			continue
		}
		rel := notePath.String()
		row, known := stored[rel]
		// Stored notes were admitted when indexed; only a new path needs the
		// ownership selector.
		if !known && policy.Admit != nil && !policy.Admit(notePath) {
			continue
		}
		delete(unseen, rel)
		selected[rel] = struct{}{}
		info, err := os.Stat(abs(rel))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if known {
				changed[rel] = cache.DirtyRemoved
			}
		case err != nil:
			return nil, false, err
		case !known:
			changed[rel] = cache.DirtyCreated
		case info.Size() != row.Size || info.ModTime().Unix() != row.Mtime:
			changed[rel] = cache.DirtyModified
		// A new event for the note voids its remembered stat, so a rewrite
		// that kept its size and exact timestamp, such as a restore, is
		// hashed once more.
		case rt.sourceVerified(rel, info, row.ContentHash, w.events(rel)):
		default:
			kind, err := contentChange(abs(rel), row)
			if err != nil {
				return nil, false, err
			}
			if kind != "" {
				changed[rel] = kind
			} else {
				rt.rememberSource(rel, info, row.ContentHash, w.events(rel))
			}
		}
	}
	// Bookkeeping follows the selected notes, so paths that were deleted,
	// renamed, or never notes do not accumulate in a long-lived runtime.
	w.pruneEvents(selected)
	rt.pruneSources(selected)
	waiting := false
	for rel := range unseen {
		if _, err := os.Stat(abs(rel)); errors.Is(err, fs.ErrNotExist) {
			changed[rel] = cache.DirtyRemoved
		} else {
			waiting = true
		}
	}
	waiting = waiting || resync
	// Note events need no batch of their own: the comparison above already
	// rehashed each note once per new event, so a late duplicate costs one read.
	for _, rel := range pending {
		waiting = waiting || classifyInternalChange(rel).any()
	}
	return changed, waiting, nil
}

// sourceWitness is the exact stat at which a note's content matched its row.
type sourceWitness struct {
	size   int64
	mtime  time.Time
	hash   string
	events uint64
}

func (rt *LiveRuntime) sourceVerified(rel string, info fs.FileInfo, hash string, events uint64) bool {
	rt.sourcesMu.Lock()
	defer rt.sourcesMu.Unlock()
	seen, ok := rt.verifiedSources[rel]
	return ok && seen.hash == hash && seen.events == events && seen.size == info.Size() && seen.mtime.Equal(info.ModTime())
}

func (rt *LiveRuntime) pruneSources(keep map[string]struct{}) {
	rt.sourcesMu.Lock()
	defer rt.sourcesMu.Unlock()
	for rel := range rt.verifiedSources {
		if _, ok := keep[rel]; !ok {
			delete(rt.verifiedSources, rel)
		}
	}
}

func (rt *LiveRuntime) rememberSource(rel string, info fs.FileInfo, hash string, events uint64) {
	rt.sourcesMu.Lock()
	defer rt.sourcesMu.Unlock()
	if rt.verifiedSources == nil {
		rt.verifiedSources = map[string]sourceWitness{}
	}
	rt.verifiedSources[rel] = sourceWitness{size: info.Size(), mtime: info.ModTime(), hash: hash, events: events}
}

// contentChange compares a stored note's file with the hash its row recorded.
func contentChange(abs string, row semdb.NoteMetadataRow) (cache.DirtyKind, error) {
	content, err := os.ReadFile(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return cache.DirtyRemoved, nil
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != row.ContentHash {
		return cache.DirtyModified, nil
	}
	return "", nil
}

// waitingInput lists paths the watcher received and whether a resync is pending.
func (w *unifiedSemanticWatcher) waitingInput() ([]string, bool) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	out := make([]string, 0, len(w.pendingByRel))
	for rel := range w.pendingByRel {
		out = append(out, rel)
	}
	return out, w.pendingResync
}

// prepareSync hands disk changes to the next batch as watcher input, preserving
// any newer events already received for the same paths.
func (w *unifiedSemanticWatcher) prepareSync(changed map[string]cache.DirtyKind) {
	for rel, kind := range changed {
		w.cacheService.MarkDirty(rel, kind)
	}
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	if w.pendingByRel == nil {
		w.pendingByRel = make(map[string]cache.DirtyKind, len(changed))
	}
	for rel, kind := range changed {
		if _, newer := w.pendingByRel[rel]; !newer {
			w.pendingByRel[rel] = kind
		}
	}
}
