package notemeta

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// selectedProviderFingerprint identifies the provider and projection versions
// that are selected for the supplied canonical note paths. It deliberately
// excludes source bytes and mtimes: RawNotesHash remains the source-only
// freshness signal.
func selectedProviderFingerprint(formats noteformat.Runtime, notePaths []string) (string, error) {
	if len(formats.Registry().IDs()) == 0 {
		return "", fmt.Errorf("note format runtime is required")
	}
	type providerEntry struct {
		Path              string `json:"path"`
		FormatID          string `json:"formatID"`
		ProviderVersion   string `json:"providerVersion"`
		ProjectionVersion string `json:"projectionVersion"`
	}
	entries := make([]providerEntry, 0, len(notePaths))
	for _, notePath := range dedupeStrings(notePaths) {
		canonical, err := paths.CleanNotePath(strings.TrimSpace(notePath))
		if err != nil {
			return "", fmt.Errorf("canonicalize selected note path %q: %w", notePath, err)
		}
		provider, ok := formats.ProviderForPath(paths.RelPath(canonical))
		if !ok {
			return "", fmt.Errorf("no note format provider selected for %q", canonical)
		}
		descriptor := provider.Descriptor()
		entries = append(entries, providerEntry{
			Path:              canonical.String(),
			FormatID:          string(descriptor.ID),
			ProviderVersion:   descriptor.ProviderVersion,
			ProjectionVersion: descriptor.ProjectionVersion,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	encoded, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("encode selected note providers: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// metadataStateHashWithSelectedProviders combines the existing raw-note and
// vault-config state fingerprint with the selected provider/version identity.
// Keeping the provider fingerprint outside RawNotesHash lets a provider update
// require a re-projection without changing source freshness semantics.
func metadataStateHashWithSelectedProviders(vaultDef obsidian.VaultDefinition, rawNotesHash string, formats noteformat.Runtime, notePaths []string) (string, error) {
	providerFingerprint, err := selectedProviderFingerprint(formats, notePaths)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	_, _ = sum.Write([]byte("note-metadata-selected-provider-v1"))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(metadataStateHash(vaultDef, rawNotesHash)))
	_, _ = sum.Write([]byte{0})
	_, _ = sum.Write([]byte(providerFingerprint))
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// metadataStateCurrentWithSelectedProviders checks raw source identity and the
// selected provider/version fingerprint together, returning the inventory it
// already read so callers never re-enumerate the vault. Provider-current row
// gates remain a separate concern for the Indexer-owned read paths.
func metadataStateCurrentWithSelectedProviders(ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, store Store, formats noteformat.Runtime) (bool, []string, error) {
	if store == nil || noteMgr == nil {
		return false, nil, nil
	}
	state, err := store.GetNoteMetadataState(ctx)
	if err != nil {
		return false, nil, err
	}
	if !state.Ready || state.LoadedAt <= 0 {
		return false, nil, nil
	}
	rawNotesHash, notePaths, err := computeNotesHash(ctx, vaultDef, noteMgr)
	if err != nil {
		return false, nil, err
	}
	expectedStateHash, err := metadataStateHashWithSelectedProviders(vaultDef, rawNotesHash, formats, notePaths)
	if err != nil {
		return false, nil, err
	}
	if state.NotesHash != expectedStateHash {
		return false, nil, nil
	}
	materialized, err := noteMetadataPathsMaterialized(ctx, store, notePaths)
	if err != nil || !materialized {
		return false, nil, err
	}
	return true, notePaths, nil
}
