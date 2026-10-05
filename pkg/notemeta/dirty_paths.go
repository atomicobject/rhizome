package notemeta

import (
	"context"
	"fmt"
	"sort"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// DirtyPaths is the bounded selected-provider note-source discovery result used
// by validation projection. Scanned counts selected notes; no project/code roots
// participate.
type DirtyPaths struct {
	Changed   []string
	Deleted   []string
	Scanned   int
	NotesHash string
}

// discoverDirtyPaths compares authored source identity with persisted rows.
// The comparison needs path, content hash, mtime, and size only, so discovery
// never runs the provider projector.
func discoverDirtyPaths(ctx context.Context, vaultDef obsidian.VaultDefinition, store Store, formats noteformat.Runtime, sources []noteformat.AuthoredSource) (*DirtyPaths, error) {
	if store == nil {
		return nil, fmt.Errorf("note metadata store is required")
	}
	currentPaths := sourcePaths(sources)
	currentByPath := make(map[string]noteformat.AuthoredSource, len(sources))
	for _, source := range sources {
		path := strings.TrimSpace(source.Path().String())
		if path != "" {
			currentByPath[path] = source
		}
	}
	persistedPaths, err := store.CurrentNoteMetadataPaths(ctx)
	if err != nil {
		return nil, err
	}
	allPaths := dedupeStrings(append(append([]string(nil), currentPaths...), persistedPaths...))
	persistedRows, err := store.CurrentNoteMetadataRowsByPaths(ctx, allPaths)
	if err != nil {
		return nil, err
	}
	state, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return nil, err
	}
	notesHash := notesHashFromSources(sources)
	currentStateHash, err := metadataStateHashWithSelectedProviders(vaultDef, notesHash, formats, currentPaths)
	if err != nil {
		return nil, err
	}
	result := &DirtyPaths{Scanned: len(currentPaths), NotesHash: currentStateHash}
	// A changed raw-note hash is expected when one note changes; row comparison
	// keeps the refresh exact. Expand every path only when the persisted state's
	// derivation/config fingerprint is itself stale for its stored raw hash.
	persistedStateHash, err := metadataStateHashWithSelectedProviders(vaultDef, state.RawNotesHash, formats, persistedPaths)
	if err != nil {
		return nil, err
	}
	forceAll := !state.Ready || state.NotesHash != persistedStateHash
	for _, notePath := range currentPaths {
		source := currentByPath[notePath]
		row, ok := persistedRows[notePath]
		if forceAll || !ok || row.Projection.Status == semdb.NoteProjectionStatusStale || row.ContentHash != source.ContentHash() || row.Mtime != source.Mtime() || row.Size != source.Size() {
			result.Changed = append(result.Changed, notePath)
		}
	}
	for _, notePath := range persistedPaths {
		if _, ok := currentByPath[notePath]; !ok {
			result.Deleted = append(result.Deleted, notePath)
		}
	}
	sort.Strings(result.Changed)
	sort.Strings(result.Deleted)
	return result, nil
}
