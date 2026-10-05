package unifiedsearch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type queryCoherenceProvider struct {
	embed func(context.Context) (embeddings.Embedding, error)
}

func (p *queryCoherenceProvider) Dimensions() int { return 8 }
func (p *queryCoherenceProvider) EmbedTexts(ctx context.Context, _ []string) ([]embeddings.Embedding, error) {
	vector, err := p.embed(ctx)
	return []embeddings.Embedding{vector}, err
}

func queryCoherenceRuntime(t *testing.T) Options {
	t.Helper()
	vault := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(vault, ".rhizome"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(vault, "pkg"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vault, ".rhizome", "config.yml"), []byte("code:\n  enabled: true\n  go:\n    roots: [pkg]\ncodeEmbeddings:\n  enabled: true\n  provider: test\n  dimensions: 8\n"), 0o644))
	store, err := semdb.Open(filepath.Join(vault, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	vectors := []embeddings.Embedding{{1, 0, 0, 0, 0, 0, 0, 0}, {0, 1, 0, 0, 0, 0, 0, 0}, {.7, .7, 0, 0, 0, 0, 0, 0}}
	for i, name := range []string{"Alpha", "Beta", "Gamma"} {
		path := "pkg/" + strings.ToLower(name) + ".go"
		require.NoError(t, os.WriteFile(filepath.Join(vault, filepath.FromSlash(path)), []byte("package pkg\n\nfunc "+name+"() {}\n"), 0o644))
		require.NoError(t, store.UpsertFileMeta(context.Background(), codeanchor.FileMeta{Path: path, Lang: codeanchor.LangGo, Hash: fmt.Sprintf("file-hash-%d", i), ParseStatus: codeanchor.ParseOK}))
		anchorID, chunkID := "anchor-"+strings.ToLower(name), "chunk-"+strings.ToLower(name)
		require.NoError(t, store.ReplaceIntelCodeFile(context.Background(), path, []codeanchor.IntelAnchor{{AnchorID: anchorID, Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: name, FQN: "pkg." + name, Signature: "func " + name + "()", StartLine: 3, EndLine: 3, Fingerprint: fmt.Sprintf("fp-%d", i)}}, nil, nil))
		require.NoError(t, store.ReplaceIntelChunks(context.Background(), []string{anchorID}, []codeanchor.IntelChunk{{ChunkID: chunkID, OwnerID: anchorID, OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: fmt.Sprintf("hash-%d", i)}}))
		require.NoError(t, store.UpsertEmbeddings(context.Background(), map[string]embeddings.Embedding{chunkID: vectors[i]}))
	}
	return Options{UseVector: true, UseIntel: true, VaultPath: vault, VaultDef: obsidian.VaultDefinition{Path: vault}, IntelStore: store,
		CodeProviderConfig: embeddings.ProviderConfig{Provider: "test", Dimensions: 8}}
}

func TestExecuteConcurrentSameTextFacetsKeepContinuation(t *testing.T) {
	runtime := queryCoherenceRuntime(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	runtime.CodeProvider = &queryCoherenceProvider{embed: func(ctx context.Context) (embeddings.Embedding, error) {
		call := calls.Add(1)
		if call == 1 {
			close(started)
		}
		select {
		case <-release:
			if call == 1 {
				return embeddings.Embedding{1, 0, 0, 0, 0, 0, 0, 0}, nil
			}
			return embeddings.Embedding{0, 1, 0, 0, 0, 0, 0, 0}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	runtime.QueryInputs = []QueryInput{{Text: "same query", Mode: string(search.IntentSearch)}, {Text: "same query", Mode: string(search.IntentOverview)}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	type reply struct {
		result ApplicationResult
		err    error
	}
	done := make(chan reply, 1)
	go func() {
		result, err := Execute(ctx, ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 1, Runtime: runtime})
		done <- reply{result, err}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("the first facet did not reach the vector provider")
	}
	close(release)
	first := <-done
	require.NoError(t, first.err)
	require.NotEmpty(t, first.result.Continuation)
	require.Equal(t, int32(1), calls.Load())
	cursor, err := DecodeContinuation(first.result.Continuation)
	require.NoError(t, err)
	require.Len(t, cursor.QueryEmbeddings, 1)
	require.Equal(t, embeddings.Embedding{1, 0, 0, 0, 0, 0, 0, 0}, cursor.QueryEmbeddings[0].Code)
	second, err := Execute(ctx, ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 1, Continuation: first.result.Continuation, Runtime: runtime})
	require.NoError(t, err)
	require.Equal(t, first.result.IndexGeneration, second.IndexGeneration)
	require.Equal(t, int32(1), calls.Load(), "both replayed facets use the cursor vector")
	require.NotEqual(t, first.result.Sources[0].Result.Path, second.Sources[0].Result.Path)
}

func TestExecuteProviderFailureRetainsOutageAndDegradedLanes(t *testing.T) {
	runtime := queryCoherenceRuntime(t)
	started := make(chan struct{})
	failure := errors.New("synthetic note provider outage")
	runtime.CodeProvider = &queryCoherenceProvider{embed: func(ctx context.Context) (embeddings.Embedding, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	runtime.NoteProvider = &queryCoherenceProvider{embed: func(ctx context.Context) (embeddings.Embedding, error) {
		select {
		case <-started:
			return nil, failure
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	runtime.NoteProviderConfig = embeddings.ProviderConfig{Provider: "test-note", Dimensions: 8}
	runtime.Query, runtime.IntentInput = "search", string(search.IntentSearch)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	result, err := Execute(ctx, ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 1, Runtime: runtime})
	require.NoError(t, err)
	require.NoError(t, ctx.Err())
	var warning string
	for _, value := range result.Warnings {
		if value.Source == "vector" {
			warning = value.Message
		}
	}
	require.Contains(t, warning, "embed note query: "+failure.Error())
	require.NotContains(t, warning, "context canceled")
	var vectorLanes int
	for _, lane := range result.Lanes {
		if lane.Lane == "note_vector" || lane.Lane == "code_vector" {
			vectorLanes++
			require.Equal(t, search.LaneStateDegraded, lane.Status)
			require.Contains(t, lane.Reason, failure.Error())
		}
	}
	require.Equal(t, 2, vectorLanes)
}
