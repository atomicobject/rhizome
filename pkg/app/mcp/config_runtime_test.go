package mcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

type mutableRuntimeView struct {
	mu            sync.RWMutex
	snapshot      runtimeview.Snapshot
	waitSemantic  func(context.Context) error
	searchWaits   atomic.Int32
	semanticWaits atomic.Int32
	codeWaits     atomic.Int32
}

func (view *mutableRuntimeView) Snapshot() runtimeview.Snapshot {
	view.mu.RLock()
	defer view.mu.RUnlock()
	return view.snapshot
}

func (view *mutableRuntimeView) set(snapshot runtimeview.Snapshot) {
	view.mu.Lock()
	view.snapshot = snapshot
	view.mu.Unlock()
}

func (view *mutableRuntimeView) WaitForSearch(context.Context) error {
	view.searchWaits.Add(1)
	return nil
}
func (view *mutableRuntimeView) WaitForSemantic(ctx context.Context) error {
	view.semanticWaits.Add(1)
	if view.waitSemantic != nil {
		return view.waitSemantic(ctx)
	}
	return nil
}
func (view *mutableRuntimeView) WaitForCodeIndex(context.Context) error {
	view.codeWaits.Add(1)
	return nil
}

func TestConfigReadsFreshPartialRuntimeSnapshot(t *testing.T) {
	view := &mutableRuntimeView{}
	cfg := Config{Runtime: view}

	require.Nil(t, cfg.GetIntelStore())
	semanticOnly := runtimeview.Snapshot{
		Semantic: runtimeview.CapabilityState{Done: true, Ready: true},
	}
	view.set(semanticOnly)
	require.Nil(t, cfg.GetIntelStore())
	require.Nil(t, cfg.getSessionDedupeStore())

	intelStore := &semdb.Store{}
	sessionStore := &semdb.Store{}
	sessionOnly := semanticOnly
	sessionOnly.SessionStore = sessionStore
	view.set(sessionOnly)
	require.Nil(t, cfg.GetIntelStore())
	require.Same(t, sessionStore, cfg.getSessionDedupeStore())

	semanticAndCode := semanticOnly
	semanticAndCode.Code = runtimeview.CapabilityState{Done: true, Ready: true}
	semanticAndCode.IntelStore = intelStore
	view.set(semanticAndCode)
	require.Same(t, intelStore, cfg.GetIntelStore())
	require.Nil(t, cfg.getSessionDedupeStore())

	semanticAndCode.SessionStore = sessionStore
	view.set(semanticAndCode)
	require.Same(t, intelStore, cfg.GetIntelStore())
	require.Same(t, sessionStore, cfg.getSessionDedupeStore())
	require.True(t, view.Snapshot().Semantic.Ready)
}

func TestConfigGetIntelStoreWithholdsUnavailableIndexedSemanticReader(t *testing.T) {
	store := &semdb.Store{}
	for _, state := range []actions.IndexedContextState{
		actions.IndexedContextMissing,
		actions.IndexedContextStale,
		actions.IndexedContextIncompatible,
	} {
		t.Run(string(state), func(t *testing.T) {
			cfg := Config{
				IntelStore:                   store,
				IndexedReadOnlySemanticQuery: true,
				IndexedContextUnavailable: &actions.IndexedContextFreshness{
					State: state,
				},
			}
			require.Nil(t, cfg.GetIntelStore())
		})
	}
}

func TestConfigTreatsValidatedProviderOnlySnapshotAsSemanticQueryReady(t *testing.T) {
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "deterministic", Dimensions: 4})
	view := &mutableRuntimeView{snapshot: runtimeview.Snapshot{NoteProvider: provider, CodeProvider: provider}}

	normal := Config{Runtime: view}
	noteOn, _, _, _ := normal.NoteEmbeddings()
	codeOn, _, _ := normal.CodeEmbeddingsState()
	require.False(t, noteOn)
	require.False(t, codeOn)

	indexed := Config{Runtime: view, IndexedReadOnlySemanticQuery: true}
	noteOn, _, noteProvider, _ := indexed.NoteEmbeddings()
	codeOn, _, codeProvider := indexed.CodeEmbeddingsState()
	require.True(t, noteOn)
	require.True(t, codeOn)
	require.Equal(t, provider, noteProvider)
	require.Equal(t, provider, codeProvider)
}

func TestSemanticQueryWaitsOnlyForSemanticPhase(t *testing.T) {
	initErr := errors.New("semantic init failed")
	view := &mutableRuntimeView{waitSemantic: func(context.Context) error { return initErr }}
	resp, err := SemanticQueryTool(Config{Runtime: view})(context.Background(), mcp.CallToolRequest{})
	require.NoError(t, err)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content[0].(mcp.TextContent).Text, initErr.Error())
	require.EqualValues(t, 1, view.semanticWaits.Load())
	require.Zero(t, view.codeWaits.Load())
}
