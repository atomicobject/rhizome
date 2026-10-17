package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/embeddings"

	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	protocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
)

func semanticPublicationFixture(t *testing.T) (Config, string) {
	t.Helper()
	root := t.TempDir()
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	cfg := Config{
		NoteMetadata: configuredMCPNoteMetadata(t),
		VaultPath:    root, VaultDef: obsidian.VaultDefinition{Path: root}, IntelStore: store, SessionStore: store, EmbeddingsOn: true, EmbedProvider: provider, CodeEmbeddingsOn: true, CodeEmbedProvider: provider}
	body := "package pkg\n\nfunc Run() {\n" + strings.Repeat("    println(\"source evidence for the selected function\")\n", 90) + "    println(\"UNIQUE_FINAL_EVIDENCE\")\n}\n"
	addSemanticPublicationSource(t, cfg, "pkg/service.go", "run", "pkg.Service.Run", body)
	return cfg, strings.TrimSpace(body)
}

func addSemanticPublicationSource(t *testing.T, cfg Config, path, id, fqn, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(cfg.VaultPath, path)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.VaultPath, path), []byte(body), 0o600))
	anchor, chunk := "anchor-"+id, "chunk-"+id
	require.NoError(t, cfg.IntelStore.UpsertFileMeta(context.Background(), codeanchor.FileMeta{Path: path, Lang: codeanchor.LangGo, Hash: "published-" + id}))
	addAnchor(t, cfg.IntelStore, cfg.CodeEmbedProvider, path, "Run", fqn, anchor, chunk)
	require.NoError(t, cfg.IntelStore.ReplaceIntelCodeFile(context.Background(), path, []codeanchor.IntelAnchor{{AnchorID: anchor, Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: "Run", FQN: fqn, Signature: "func Run()", StartLine: 3, EndLine: int64(strings.Count(body, "\n")), Fingerprint: "fp-" + id}}, nil, nil))
	require.NoError(t, cfg.IntelStore.ReplaceIntelChunks(context.Background(), []string{anchor}, []codeanchor.IntelChunk{{ChunkID: chunk, OwnerID: anchor, OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "content-" + id}}))
	require.NoError(t, cfg.IntelStore.UpsertEmbeddings(context.Background(), map[string]embeddings.Embedding{chunk: embedSingle(t, cfg.CodeEmbedProvider, "Run")}))
	generation, err := unifiedsearch.IndexGeneration(context.Background(), cfg.IntelStore, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, generation)
}

func semanticPublicationCall(ctx context.Context, cfg Config, session string, queries ...string) ([]byte, error) {
	if len(queries) == 0 {
		queries = []string{"pkg.Service.Run"}
	}
	inputs := make([]any, len(queries))
	for i, query := range queries {
		inputs[i] = map[string]any{"text": query, "mode": "go_to_def"}
	}
	result, err := SemanticQueryTool(cfg)(ctx, protocol.CallToolRequest{Params: protocol.CallToolParams{Name: "semantic_query", Arguments: map[string]any{
		"queries": inputs, "scope": "code", "sessionId": session,
	}}})
	if err != nil {
		return nil, err
	}
	text := result.Content[0].(protocol.TextContent).Text
	if result.IsError {
		return nil, fmt.Errorf("tool failed: %s", text)
	}
	return []byte(text), nil
}

func decodeSemanticPublication(t *testing.T, data []byte, err error) semanticQueryResponse {
	t.Helper()
	require.NoError(t, err)
	var out semanticQueryResponse
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

func publicationSourcePair(t *testing.T, out semanticQueryResponse, path, anchorID, fqn string) (anchor, file SemanticMatchPayload) {
	t.Helper()
	var ids []string
	for _, match := range out.Matches {
		if match.Path != path {
			continue
		}
		require.Equal(t, "code", match.Type)
		ids = append(ids, match.AnchorID)
		if match.AnchorID == "" {
			file = match
			require.Empty(t, file.FQN)
		} else if match.AnchorID == anchorID {
			anchor = match
			require.Equal(t, fqn, anchor.FQN)
		}
	}
	require.ElementsMatch(t, []string{"", anchorID}, ids)
	return anchor, file
}

func TestDetailedSemanticSessionPublishesOnlyDeliveredBodies(t *testing.T) {
	for _, budgets := range [][2]int{{8000, 30000}, {2500, 6000}, {12000, 30000}} {
		t.Run(fmt.Sprint(budgets[0]), func(t *testing.T) {
			cfg, full := semanticPublicationFixture(t)
			cfg.BudgetCharsOverride = budgets[0]
			data, err := semanticPublicationCall(context.Background(), cfg, "delivery")
			first := decodeSemanticPublication(t, data, err)
			require.LessOrEqual(t, len(data), budgets[0])
			require.Len(t, first.Matches, 2)
			firstAnchor, firstFile := publicationSourcePair(t, first, "pkg/service.go", "anchor-run", "pkg.Service.Run")
			require.Nil(t, compactSourceBody(firstFile), "bounded file preview does not publish a source body")
			fp, seen, err := cfg.IntelStore.SessionItemFingerprint(context.Background(), "delivery", "code:pkg/service.go")
			require.NoError(t, err)
			if budgets[0] == 12000 {
				require.Equal(t, full, firstAnchor.FullFileContent)
				require.True(t, seen)
				require.Equal(t, fingerprintText(full), fp)
			} else {
				require.Empty(t, firstAnchor.FullFileContent)
				require.Empty(t, firstAnchor.ExcerptContent)
				require.False(t, seen, "considered bodies and serialized skeletons cannot publish delivery")
			}
			cfg.BudgetCharsOverride = budgets[1]
			data, err = semanticPublicationCall(context.Background(), cfg, "delivery")
			larger := decodeSemanticPublication(t, data, err)
			freshData, err := semanticPublicationCall(context.Background(), cfg, "fresh")
			fresh := decodeSemanticPublication(t, freshData, err)
			require.Len(t, larger.Matches, 2)
			largerAnchor, _ := publicationSourcePair(t, larger, "pkg/service.go", "anchor-run", "pkg.Service.Run")
			freshAnchor, _ := publicationSourcePair(t, fresh, "pkg/service.go", "anchor-run", "pkg.Service.Run")
			if budgets[0] == 12000 {
				require.True(t, largerAnchor.ContentDeduped)
				require.Empty(t, largerAnchor.FullFileContent)
				require.NotContains(t, larger.Text, "source evidence for the selected function")
			} else {
				require.Equal(t, compactSourceBody(freshAnchor), compactSourceBody(largerAnchor))
				body := compactSourceBody(largerAnchor)
				require.NotNil(t, body)
				require.NotEmpty(t, body.Content)
				fp, seen, err = cfg.IntelStore.SessionItemFingerprint(context.Background(), "delivery", "code:pkg/service.go")
				require.NoError(t, err)
				require.True(t, seen)
				require.Equal(t, fingerprintText(body.Content), fp)
			}
			require.Equal(t, firstAnchor.Path, largerAnchor.Path)
			require.Equal(t, firstAnchor.Score, largerAnchor.Score)
		})
	}
}

func TestDetailedSemanticSessionPrefixDoesNotSuppressLargerBody(t *testing.T) {
	cfg, full := semanticPublicationFixture(t)
	cfg.BudgetCharsOverride = 6000
	data, err := semanticPublicationCall(context.Background(), cfg, "prefix")
	first := decodeSemanticPublication(t, data, err)
	require.NotEmpty(t, first.Matches[0].ExcerptContent)
	require.NotEqual(t, full, first.Matches[0].ExcerptContent)
	data, err = semanticPublicationCall(context.Background(), cfg, "prefix")
	repeated := decodeSemanticPublication(t, data, err)
	require.True(t, repeated.Matches[0].ContentDeduped)
	require.Empty(t, repeated.Matches[0].ExcerptContent)
	cfg.BudgetCharsOverride = 30000
	data, err = semanticPublicationCall(context.Background(), cfg, "prefix")
	larger := decodeSemanticPublication(t, data, err)
	require.Equal(t, full, larger.Matches[0].FullFileContent)
	require.False(t, larger.Matches[0].ContentDeduped)
}

func TestDetailedSemanticLegacyCommentBytesUseDeliveredBodyIdentity(t *testing.T) {
	cfg, _ := semanticPublicationFixture(t)
	body := "package pkg\n\nfunc Run() {\n // legacy comment " + string([]byte{0xff, 0xfe, 0xff}) + "\n" + strings.Repeat(" println(\"WIRE_BODY_UNIQUE\")\n", 50) + "}\n"
	addSemanticPublicationSource(t, cfg, "pkg/service.go", "run", "pkg.Service.Run", body)
	cfg.BudgetCharsOverride = 30000
	data, err := semanticPublicationCall(context.Background(), cfg, "wire-normalization")
	first := decodeSemanticPublication(t, data, err)
	// Match encoding/json's actual wire representation, including consecutive
	// invalid bytes, rather than treating one invalid run as one replacement.
	wire, err := json.Marshal(strings.TrimSpace(body))
	require.NoError(t, err)
	var expected string
	require.NoError(t, json.Unmarshal(wire, &expected))
	require.Equal(t, expected, first.Matches[0].FullFileContent)
	require.True(t, utf8.ValidString(first.Text))
	require.Contains(t, first.Text, "WIRE_BODY_UNIQUE")
	fp, seen, err := cfg.IntelStore.SessionItemFingerprint(context.Background(), "wire-normalization", "code:pkg/service.go")
	require.NoError(t, err)
	require.True(t, seen)
	require.Equal(t, fingerprintText(expected), fp)
	data, err = semanticPublicationCall(context.Background(), cfg, "wire-normalization")
	repeated := decodeSemanticPublication(t, data, err)
	require.True(t, repeated.Matches[0].ContentDeduped)
	require.Empty(t, repeated.Matches[0].FullFileContent)
	require.NotContains(t, repeated.Text, "WIRE_BODY_UNIQUE")
}

func TestConcurrentDetailedSemanticHandlerPublishesEachBodyOnce(t *testing.T) {
	cfg, full := semanticPublicationFixture(t)
	cfg.BudgetCharsOverride = 12000 // Full structured body survives while packed text is clipped.
	const count = 8
	barrier := &synchronizedFingerprintStore{SessionDedupeStore: cfg.SessionStore}
	barrier.readers.Add(count)
	cfg.SessionStore = barrier
	type result struct {
		data []byte
		err  error
	}
	results := make(chan result, count)
	for range count {
		go func() {
			data, err := semanticPublicationCall(context.Background(), cfg, "concurrent-detailed")
			results <- result{data, err}
		}()
	}
	winners := 0
	for range count {
		result := <-results
		out := decodeSemanticPublication(t, result.data, result.err)
		require.Len(t, out.Matches, 2)
		match, file := publicationSourcePair(t, out, "pkg/service.go", "anchor-run", "pkg.Service.Run")
		require.Nil(t, compactSourceBody(file), "the file preview is independent of function-body reservation")
		if match.FullFileContent != "" {
			winners++
			require.Equal(t, full, match.FullFileContent)
			require.Contains(t, out.Text, "source evidence for the selected function")
		} else {
			require.True(t, match.ContentDeduped)
			require.False(t, match.Included)
			require.NotContains(t, out.Text, "source evidence for the selected function")
		}
	}
	require.Equal(t, 1, winners)
	fp, seen, err := cfg.IntelStore.SessionItemFingerprint(context.Background(), "concurrent-detailed", "code:pkg/service.go")
	require.NoError(t, err)
	require.True(t, seen)
	require.Equal(t, fingerprintText(full), fp)
}

type cancelSemanticReservationStore struct {
	semdb.SessionDedupeStore
	cancel context.CancelFunc
}

func (s *cancelSemanticReservationStore) ReserveSessionItems(ctx context.Context, session, owner string, items []semdb.SessionItem) ([]semdb.SessionItem, error) {
	reserved, err := s.SessionDedupeStore.ReserveSessionItems(ctx, session, owner, items)
	s.cancel()
	return reserved, err
}

func TestDetailedSemanticCanceledPublicationReleasesOwnership(t *testing.T) {
	cfg, full := semanticPublicationFixture(t)
	cfg.BudgetCharsOverride = 30000
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg.SessionStore = &cancelSemanticReservationStore{SessionDedupeStore: cfg.IntelStore, cancel: cancel}
	_, err := semanticPublicationCall(ctx, cfg, "canceled")
	require.ErrorContains(t, err, "context canceled")
	cfg.SessionStore = cfg.IntelStore
	data, err := semanticPublicationCall(context.Background(), cfg, "canceled")
	out := decodeSemanticPublication(t, data, err)
	require.Equal(t, full, out.Matches[0].FullFileContent)
	require.False(t, out.Matches[0].ContentDeduped)
}

func TestDetailedSemanticFailedFinalEncodingReleasesOwnership(t *testing.T) {
	cfg, _ := semanticPublicationFixture(t)
	tracker := &sessionTracker{ctx: context.Background(), store: cfg.SessionStore, sessionID: "failed-encoding"}
	resp := semanticQueryResponse{Count: 3, Matches: []SemanticMatchPayload{
		{Type: "code", Path: "a", ContentKind: "full", FullFileContent: "x"},
		{Type: "code", Path: "b", ContentKind: "full", FullFileContent: "unseen body"},
		{Type: "code", Path: "b", ContentKind: "excerpt", ExcerptContent: "unseen excerpt"},
	}}
	tracker.MarkSent("code:a", fingerprintText("x"))
	encoded, err := json.Marshal(resp)
	require.NoError(t, err)
	_, err = finalizeSemanticQueryResponse(tracker, resp, encoded, len(encoded))
	require.ErrorContains(t, err, "dedupe qualifications")
	groups := detailedSemanticDeliveryGroups(resp)
	item := groups[1].item
	reservation := tracker.Reserve([]actions.DedupeItem{item})
	require.True(t, reservation.Allowed(item.Key, item.Fingerprint), "failed encoding must retire its active reservation")
	reservation.Commit(nil)
	_, err = finalizeSemanticQueryResponse(tracker, resp, []byte("{"), 5000)
	require.Error(t, err)
	data, err := finalizeSemanticQueryResponse(tracker, resp, encoded, 5000)
	out := decodeSemanticPublication(t, data, err)
	require.True(t, out.Matches[0].ContentDeduped)
	require.Equal(t, "unseen body", out.Matches[1].FullFileContent)
	require.Equal(t, "unseen excerpt", out.Matches[2].ExcerptContent)
}

func TestDetailedSemanticSuppressionKeepsUTF8TrimMarkerAndOtherSources(t *testing.T) {
	// The trim marker and original arrow share an initial UTF-8 byte. A common
	// prefix must stop at the complete rune before applying renderer byte ranges.
	body := "blocked\n→tail"
	original := semanticQueryResponse{Text: "answer\nheader\n" + body + "\nother source"}
	start := len("answer\nheader\n")
	item := actions.DedupeItem{Key: "code:a", Fingerprint: fingerprintText(body)}
	original.textBodies = []semanticTextBody{{key: item.Key, fingerprint: item.Fingerprint, start: start, end: start + len(body)}}
	trimmed := "answer\nheader\nblocked\n…"
	out := suppressSemanticTextBodies(original, trimmed, map[actions.DedupeItem]bool{item: true})
	require.True(t, utf8.ValidString(out))
	require.Equal(t, "answer\nheader\n…", out)
	out = suppressSemanticTextBodies(original, original.Text, map[actions.DedupeItem]bool{item: true})
	require.Equal(t, "answer\nheader\n\nother source", out)
}

type competingSemanticReservationStore struct {
	semdb.SessionDedupeStore
	blocked semdb.SessionItem
}

func (s *competingSemanticReservationStore) ReserveSessionItems(ctx context.Context, session, owner string, items []semdb.SessionItem) ([]semdb.SessionItem, error) {
	// Acquire a competing owner after candidate inspection, before publication.
	if _, err := s.SessionDedupeStore.ReserveSessionItems(ctx, session, "competing-owner", []semdb.SessionItem{s.blocked}); err != nil {
		return nil, err
	}
	return s.SessionDedupeStore.ReserveSessionItems(ctx, session, owner, items)
}

func TestDetailedSemanticGroupedTextSuppressesOnlyTheCompetingSource(t *testing.T) {
	cfg, _ := semanticPublicationFixture(t)
	second := "package other\n\nfunc Run() {\n" + strings.Repeat("    println(\"orientation Δ界\")\n", 20) + "    println(\"SECOND_UNIQUE_Δ界\")\n}\n"
	addSemanticPublicationSource(t, cfg, "other/service.go", "other", "pkg.Other.Run", second)
	cfg.BudgetCharsOverride = 60000
	data, err := semanticPublicationCall(context.Background(), cfg, "", "pkg.Service.Run", "pkg.Other.Run")
	control := decodeSemanticPublication(t, data, err)
	require.ElementsMatch(t, []string{"pkg/service.go", "pkg/service.go", "other/service.go", "other/service.go"}, semanticMatchPaths(control.Matches))
	publicationSourcePair(t, control, "pkg/service.go", "anchor-run", "pkg.Service.Run")
	blocked, _ := publicationSourcePair(t, control, "other/service.go", "anchor-other", "pkg.Other.Run")
	require.Contains(t, control.Text, "UNIQUE_FINAL_EVIDENCE")
	require.Contains(t, control.Text, "SECOND_UNIQUE_Δ界")
	// Deny the other source so suppression must retain the preceding
	// answer text, group header, and complete source without shifting its bytes.
	require.NotNil(t, compactSourceBody(blocked))
	cfg.SessionStore = &competingSemanticReservationStore{SessionDedupeStore: cfg.IntelStore, blocked: semdb.SessionItem{Key: semanticMatchKey(blocked), Fingerprint: fingerprintText(compactSourceBody(blocked).Content)}}
	data, err = semanticPublicationCall(context.Background(), cfg, "grouped", "pkg.Service.Run", "pkg.Other.Run")
	out := decodeSemanticPublication(t, data, err)
	require.Equal(t, semanticMatchPaths(control.Matches), semanticMatchPaths(out.Matches))
	publicationSourcePair(t, out, "pkg/service.go", "anchor-run", "pkg.Service.Run")
	publicationSourcePair(t, out, "other/service.go", "anchor-other", "pkg.Other.Run")
	require.True(t, utf8.ValidString(out.Text))
	for i, match := range out.Matches {
		require.Equal(t, control.Matches[i].Score, match.Score)
		marker := "UNIQUE_FINAL_EVIDENCE"
		if match.Path == "other/service.go" {
			marker = "SECOND_UNIQUE_Δ界"
		}
		if match.Path == "other/service.go" {
			require.True(t, match.ContentDeduped)
			require.Empty(t, compactSourceBody(match).Content)
			require.NotContains(t, out.Text, marker)
		} else {
			require.Equal(t, compactSourceBody(control.Matches[i]).Content, compactSourceBody(match).Content)
			require.Contains(t, out.Text, marker)
		}
	}
	require.NoError(t, cfg.IntelStore.ReleaseSessionReservations(context.Background(), "grouped", "competing-owner"))
}
