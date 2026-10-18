package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/embeddings"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
)

func semanticSameFileFixture(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	store := newIntelStore(t)
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	cfg := Config{
		NoteMetadata: configuredMCPNoteMetadata(t),
		VaultPath:    root, VaultDef: obsidian.VaultDefinition{Path: root}, IntelStore: store, SessionStore: store, EmbeddingsOn: true, EmbedProvider: provider, CodeEmbeddingsOn: true, CodeEmbedProvider: provider}
	alpha := "func Alpha() {\n" + strings.Repeat(" println(\"ALPHA evidence only for the first selected function\")\n", 200) + " println(\"ALPHA_END\")\n}\n"
	beta := "func Beta() {\n" + strings.Repeat(" println(\"BETA independent behavior of the second function\")\n", 200) + " println(\"BETA_END\")\n}\n"
	body := "package pkg\n\n" + alpha + "\n" + beta
	require.NoError(t, os.WriteFile(filepath.Join(root, "service.go"), []byte(body), 0o600))
	anchors := []codeanchor.IntelAnchor{
		{AnchorID: "alpha", Lang: codeanchor.LangGo, Kind: "function", Path: "service.go", Symbol: "Alpha", FQN: "pkg.Alpha", Signature: "func Alpha()", StartByte: 13, EndByte: int64(13 + len(alpha)), StartLine: 3, EndLine: int64(2 + strings.Count(alpha, "\n")), Fingerprint: "fp-alpha"},
		{AnchorID: "beta", Lang: codeanchor.LangGo, Kind: "function", Path: "service.go", Symbol: "Beta", FQN: "pkg.Beta", Signature: "func Beta()", StartByte: int64(14 + len(alpha)), EndByte: int64(len(body)), StartLine: int64(4 + strings.Count(alpha, "\n")), EndLine: int64(strings.Count(body, "\n")), Fingerprint: "fp-beta"},
	}
	ctx := context.Background()
	require.NoError(t, store.UpsertFileMeta(ctx, codeanchor.FileMeta{Path: "service.go", Lang: codeanchor.LangGo, Hash: "published"}))
	require.NoError(t, store.ReplaceIntelCodeFile(ctx, "service.go", anchors, nil, nil))
	require.NoError(t, store.ReplaceIntelChunks(ctx, []string{"alpha", "beta"}, []codeanchor.IntelChunk{
		{ChunkID: "alpha-chunk", OwnerID: "alpha", OwnerType: "anchor", Granularity: "symbol", ContentHash: "alpha-content"},
		{ChunkID: "beta-chunk", OwnerID: "beta", OwnerType: "anchor", Granularity: "symbol", ContentHash: "beta-content"},
	}))
	require.NoError(t, store.UpsertEmbeddings(ctx, map[string]embeddings.Embedding{"alpha-chunk": embedSingle(t, provider, "Alpha"), "beta-chunk": embedSingle(t, provider, "Beta")}))
	generation, err := unifiedsearch.IndexGeneration(ctx, store, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, generation)
	return cfg
}

// The indexed file remains a selected source alongside its function anchors.
// These budgets select function bodies while leaving the file as a preview.
func sameFileAnchorMatches(t *testing.T, out semanticQueryResponse, anchorIDs ...string) []SemanticMatchPayload {
	t.Helper()
	ids := make([]string, 0, len(out.Matches))
	byID := make(map[string]SemanticMatchPayload, len(out.Matches))
	for _, match := range out.Matches {
		require.Equal(t, "code", match.Type)
		require.Equal(t, "service.go", match.Path)
		ids = append(ids, match.AnchorID)
		byID[match.AnchorID] = match
	}
	require.ElementsMatch(t, append([]string{""}, anchorIDs...), ids)
	file := byID[""]
	require.Empty(t, file.FQN)
	require.Nil(t, compactSourceBody(file), "bounded file preview does not claim a delivered source body")
	require.Empty(t, file.FullFileContent)
	require.Empty(t, file.ExcerptContent)
	require.Empty(t, file.OutlineContent)
	require.Empty(t, file.SignatureContent)
	require.Empty(t, file.StubContent)
	require.False(t, file.ContentDeduped, "an undelivered file preview cannot claim prior delivery")
	anchors := make([]SemanticMatchPayload, 0, len(anchorIDs))
	for _, id := range anchorIDs {
		match := byID[id]
		require.Equal(t, map[string]string{"alpha": "pkg.Alpha", "beta": "pkg.Beta"}[id], match.FQN)
		require.Equal(t, map[string]string{"alpha": "Alpha", "beta": "Beta"}[id], match.Symbol)
		anchors = append(anchors, match)
	}
	return anchors
}

func TestDetailedSemanticSameFileRepresentationsPublishTogether(t *testing.T) {
	for _, budget := range []int{8000, 40000, 60000} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			cfg := semanticSameFileFixture(t)
			cfg.BudgetCharsOverride = budget
			data, err := semanticPublicationCall(context.Background(), cfg, "", "pkg.Alpha", "pkg.Beta")
			control := decodeSemanticPublication(t, data, err)
			controlAnchors := sameFileAnchorMatches(t, control, "alpha", "beta")
			data, err = semanticPublicationCall(context.Background(), cfg, "same-file", "pkg.Alpha", "pkg.Beta")
			first := decodeSemanticPublication(t, data, err)
			firstAnchors := sameFileAnchorMatches(t, first, "alpha", "beta")
			require.Equal(t, 0, first.DedupeHits)
			for i, match := range controlAnchors {
				require.NotNil(t, compactSourceBody(match))
				require.NotEmpty(t, compactSourceBody(match).Content)
				require.Equal(t, compactSourceBody(match), compactSourceBody(firstAnchors[i]))
			}
			fp, seen, err := cfg.IntelStore.SessionItemFingerprint(context.Background(), "same-file", "code:service.go")
			require.NoError(t, err)
			require.True(t, seen)
			if budget == 60000 { // Identical full bodies keep ordinary single-body history.
				require.Equal(t, firstAnchors[0].FullFileContent, firstAnchors[1].FullFileContent)
				require.Equal(t, fingerprintText(firstAnchors[0].FullFileContent), fp)
			} else {
				require.NotEqual(t, fingerprintText(compactSourceBody(firstAnchors[0]).Content), fp)
				require.NotEqual(t, fingerprintText(compactSourceBody(firstAnchors[1]).Content), fp)
			}
			data, err = semanticPublicationCall(context.Background(), cfg, "same-file", "pkg.Beta", "pkg.Alpha")
			repeated := decodeSemanticPublication(t, data, err)
			for _, match := range sameFileAnchorMatches(t, repeated, "alpha", "beta") {
				require.True(t, match.ContentDeduped)
				require.False(t, match.Included)
				require.NotNil(t, compactSourceBody(match))
				require.Empty(t, compactSourceBody(match).Content)
			}
			require.NotContains(t, repeated.Text, "func Alpha()")
			require.NotContains(t, repeated.Text, "func Beta()")
		})
	}
}

func TestDetailedSemanticSameFileSubsetRemainsEligible(t *testing.T) {
	cfg := semanticSameFileFixture(t)
	cfg.BudgetCharsOverride = 8000
	data, err := semanticPublicationCall(context.Background(), cfg, "subset", "pkg.Alpha", "pkg.Beta")
	group := decodeSemanticPublication(t, data, err)
	sameFileAnchorMatches(t, group, "alpha", "beta")
	data, err = semanticPublicationCall(context.Background(), cfg, "", "pkg.Alpha")
	control := decodeSemanticPublication(t, data, err)
	controlAnchor := sameFileAnchorMatches(t, control, "alpha")[0]
	data, err = semanticPublicationCall(context.Background(), cfg, "subset", "pkg.Alpha")
	subset := decodeSemanticPublication(t, data, err)
	subsetAnchor := sameFileAnchorMatches(t, subset, "alpha")[0]
	require.NotNil(t, compactSourceBody(controlAnchor))
	require.Equal(t, compactSourceBody(controlAnchor), compactSourceBody(subsetAnchor), "group history cannot falsely claim another selected representation was delivered")
	data, err = semanticPublicationCall(context.Background(), cfg, "subset", "pkg.Alpha")
	repeated := decodeSemanticPublication(t, data, err)
	require.True(t, sameFileAnchorMatches(t, repeated, "alpha")[0].ContentDeduped)
	// An individually delivered sibling cannot suppress a different sibling.
	data, err = semanticPublicationCall(context.Background(), cfg, "subset", "pkg.Beta")
	sibling := decodeSemanticPublication(t, data, err)
	siblingAnchor := sameFileAnchorMatches(t, sibling, "beta")[0]
	require.False(t, siblingAnchor.ContentDeduped)
	require.NotNil(t, compactSourceBody(siblingAnchor))
	require.NotEmpty(t, compactSourceBody(siblingAnchor).Content)

	// A subset can replace the one stored source fingerprint. Reconstitute the
	// represented group once, then identical/reordered requests must converge.
	for step, queries := range [][]string{{"pkg.Alpha", "pkg.Beta"}, {"pkg.Beta", "pkg.Alpha"}, {"pkg.Alpha", "pkg.Beta"}} {
		data, err = semanticPublicationCall(context.Background(), cfg, "subset", queries...)
		out := decodeSemanticPublication(t, data, err)
		for _, match := range sameFileAnchorMatches(t, out, "alpha", "beta") {
			require.NotNil(t, compactSourceBody(match))
			if step == 0 {
				require.NotEmpty(t, compactSourceBody(match).Content)
				require.False(t, match.ContentDeduped)
			} else {
				require.Empty(t, compactSourceBody(match).Content)
				require.True(t, match.ContentDeduped)
				require.False(t, match.Included)
			}
		}
		if step > 0 {
			require.NotContains(t, out.Text, "func Alpha()")
			require.NotContains(t, out.Text, "func Beta()")
		}
	}
}

type synchronizedSemanticReservationStore struct {
	semdb.SessionDedupeStore
	prepared, reserved sync.WaitGroup
}

func (s *synchronizedSemanticReservationStore) ReserveSessionItems(ctx context.Context, session, owner string, items []semdb.SessionItem) ([]semdb.SessionItem, error) {
	// All requests finish selection first; hold the winning owner until every
	// competing group/subset has attempted its real SQLite reservation.
	s.prepared.Done()
	s.prepared.Wait()
	reserved, err := s.SessionDedupeStore.ReserveSessionItems(ctx, session, owner, items)
	s.reserved.Done()
	s.reserved.Wait()
	return reserved, err
}

func TestConcurrentDetailedSemanticSameFileGroupsAndSubsetsShareOneOwner(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			cfg := semanticSameFileFixture(t)
			cfg.BudgetCharsOverride = 8000
			const count = 6
			barrier := &synchronizedSemanticReservationStore{SessionDedupeStore: cfg.SessionStore}
			barrier.prepared.Add(count)
			barrier.reserved.Add(count)
			cfg.SessionStore = barrier
			type result struct {
				data    []byte
				err     error
				anchors []string
			}
			results := make(chan result, count)
			for i := range count {
				queries := []string{"pkg.Alpha", "pkg.Beta"}
				anchors := []string{"alpha", "beta"}
				if mixed && i%2 == 0 {
					queries = queries[:1]
					anchors = anchors[:1]
				}
				go func() {
					data, err := semanticPublicationCall(context.Background(), cfg, "same-owner", queries...)
					results <- result{data, err, anchors}
				}()
			}
			winners := 0
			for range count {
				result := <-results
				out := decodeSemanticPublication(t, result.data, result.err)
				anchors := sameFileAnchorMatches(t, out, result.anchors...)
				require.NotNil(t, compactSourceBody(anchors[0]))
				if compactSourceBody(anchors[0]).Content != "" {
					winners++
					for _, match := range anchors {
						require.NotNil(t, compactSourceBody(match))
						require.NotEmpty(t, compactSourceBody(match).Content)
						require.False(t, match.ContentDeduped)
					}
					require.Contains(t, out.Text, "func Alpha()")
					if len(result.anchors) == 2 {
						require.Contains(t, out.Text, "func Beta()")
					}
				} else {
					for _, match := range anchors {
						require.NotNil(t, compactSourceBody(match))
						require.Empty(t, compactSourceBody(match).Content)
						require.True(t, match.ContentDeduped)
						require.False(t, match.Included)
					}
					require.NotContains(t, out.Text, "func Alpha()")
					require.NotContains(t, out.Text, "func Beta()")
				}
			}
			require.Equal(t, 1, winners)
		})
	}
}
