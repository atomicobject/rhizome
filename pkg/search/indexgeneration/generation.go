// Package indexgeneration identifies the committed source and embedding state
// visible to a search request.
package indexgeneration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	codeembsql "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex/sqlite"
	noteembsql "github.com/atomicobject/rhizome/pkg/search/embeddings/sqlite"
)

var ErrUnavailable = fmt.Errorf("search index generation unavailable")

func Current(ctx context.Context, intel *semdb.Store, notes *noteembsql.Store, code *codeembsql.Store) (string, error) {
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
			return "", fmt.Errorf("%w: note embeddings have no committed visible generation", ErrUnavailable)
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
			return "", fmt.Errorf("%w: code embeddings have no committed visible generation", ErrUnavailable)
		}
		codeGeneration = generation
		available = true
	}
	if !available {
		return "", ErrUnavailable
	}
	payload := fmt.Sprintf("notes=%s\x00raw=%s\x00code-corpus=%s\x00embeddings=%s\x00note-gen=%d\x00code-gen=%d\x00scope=%s\x00indexer=%s", noteHash, rawNoteHash, codeCorpusHash, embeddingHash, noteGeneration, codeGeneration, scopeHash, indexerVersion)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:]), nil
}
