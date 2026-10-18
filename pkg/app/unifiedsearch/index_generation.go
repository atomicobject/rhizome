package unifiedsearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	codeembsql "github.com/atomicobject/rhizome/pkg/embeddings/sqlite"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

var ErrIndexGenerationUnavailable = fmt.Errorf("search index generation unavailable")

// IndexGeneration is a composite identity for every persisted source used by
// unified search. NoteMetadataState hashes change when indexed note ownership,
// content, or provider regions change. The embedding stores publish visible
// generations only after their write batches commit. Scope and indexer version
// cover code eligibility and index semantics. This uses existing metadata and
// adds no persistence schema.
func IndexGeneration(ctx context.Context, intel *semdb.Store, notes *codeembsql.NoteStore, code *codeembsql.CodeStore) (string, error) {
	noteHash, rawNoteHash, codeCorpusHash, embeddingHash, scopeHash, indexerVersion := "", "", "", "", "", ""
	var noteGeneration, codeGeneration int64
	available := false
	if intel != nil {
		state, err := intel.GetNoteMetadataState(ctx)
		if err != nil {
			return "", err
		}
		noteHash, rawNoteHash = state.NotesHash, state.RawNotesHash
		if state.Ready && noteHash != "" && rawNoteHash != "" {
			available = true
		}
		var codeCount int
		codeCorpusHash, codeCount, err = intel.SearchCodeCorpusFingerprint(ctx)
		if err != nil {
			return "", err
		}
		available = available || codeCount > 0
		var embeddingCount int
		embeddingHash, embeddingCount, err = intel.SearchEmbeddingFingerprint(ctx)
		if err != nil {
			return "", err
		}
		available = available || embeddingCount > 0
		scopeHash, _, err = intel.GetScopeConfigHash(ctx)
		if err != nil {
			return "", err
		}
		indexerVersion, _, err = intel.IndexerVersion(ctx)
		if err != nil {
			return "", err
		}
	}
	if notes != nil {
		generation, ok, err := notes.VisibleGeneration(ctx)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("%w: note embeddings have no committed visible generation", ErrIndexGenerationUnavailable)
		}
		noteGeneration = generation
		available = true
	}
	if code != nil {
		generation, ok, err := code.VisibleGeneration(ctx)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("%w: code embeddings have no committed visible generation", ErrIndexGenerationUnavailable)
		}
		codeGeneration = generation
		available = true
	}
	if !available {
		return "", ErrIndexGenerationUnavailable
	}
	payload := fmt.Sprintf("notes=%s\x00raw=%s\x00code-corpus=%s\x00embeddings=%s\x00note-gen=%d\x00code-gen=%d\x00scope=%s\x00indexer=%s", noteHash, rawNoteHash, codeCorpusHash, embeddingHash, noteGeneration, codeGeneration, scopeHash, indexerVersion)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:]), nil
}
