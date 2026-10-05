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
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/indexgeneration"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	protocol "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func semanticPublicationFixture(t *testing.T) (Config, string) {
	t.Helper()
	root := t.TempDir()
	provider := embeddings.NewDeterministicProvider(embeddings.ProviderConfig{Provider: "test", Dimensions: 8})
	store := newIntelStore(t)
	cfg := Config{VaultPath: root, VaultDef: obsidian.VaultDefinition{Path: root}, IntelStore: store, SessionStore: store, EmbeddingsOn: true, EmbedProvider: provider, CodeEmbeddingsOn: true, CodeEmbedProvider: provider}
	body := "package pkg\n\nfunc Run() {\n" + strings.Repeat("    println(\"source evidence for the selected function\")\n", 90) + "    println(\"UNIQUE_FINAL_EVIDENCE\")\n}\n"
	addSemanticPublicationSource(t, cfg, "pkg/service.go", "run", "pkg.Service.Run", body)
	return cfg, strings.TrimSpace(body)
}

func addSemanticPublicationSource(t *testing.T, cfg Config, path, id, fqn, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(cfg.VaultPath, path)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cfg.VaultPath, path), []byte(body), 0o600))
	anchor, chunk := "anchor-"+id, "chunk-"+id
	addAnchor(t, cfg.IntelStore, cfg.CodeEmbedProvider, path, "Run", fqn, anchor, chunk)
	require.NoError(t, cfg.IntelStore.ReplaceIntelCodeFile(context.Background(), path, []codeanchor.IntelAnchor{{AnchorID: anchor, Lang: codeanchor.LangGo, Kind: "function", Path: path, Symbol: "Run", FQN: fqn, Signature: "func Run()", StartLine: 3, EndLine: int64(strings.Count(body, "\n")), Fingerprint: "fp-" + id}}, nil, nil))
	require.NoError(t, cfg.IntelStore.ReplaceIntelChunks(context.Background(), []string{anchor}, []codeanchor.IntelChunk{{ChunkID: chunk, OwnerID: anchor, OwnerType: "anchor", Ord: 0, Granularity: "symbol", ContentHash: "content-" + id}}))
	require.NoError(t, cfg.IntelStore.UpsertEmbeddings(context.Background(), map[string]embeddings.Embedding{chunk: embedSingle(t, cfg.CodeEmbedProvider, "Run")}))
	generation, err := indexgeneration.Current(context.Background(), cfg.IntelStore, nil, nil)
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

func TestDetailedSemanticSessionPublishesOnlyDeliveredBodies(t *testing.T) {
	for _, budgets := range [][2]int{{8000, 30000}, {2500, 6000}, {12000, 30000}} {
		t.Run(fmt.Sprint(budgets[0]), func(t *testing.T) {
			cfg, full := semanticPublicationFixture(t)
			cfg.BudgetCharsOverride = budgets[0]
			data, err := semanticPublicationCall(context.Background(), cfg, "delivery")
			first := decodeSemanticPublication(t, data, err)
			require.LessOrEqual(t, len(data), budgets[0])
			require.Len(t, first.Matches, 1)
			fp, seen, err := cfg.IntelStore.SessionItemFingerprint(context.Background(), "delivery", "code:pkg/service.go")
			require.NoError(t, err)
			if budgets[0] == 12000 {
				require.Equal(t, full, first.Matches[0].FullFileContent)
				require.True(t, seen)
				require.Equal(t, fingerprintText(full), fp)
			} else {
				require.Empty(t, first.Matches[0].FullFileContent)
				require.Empty(t, first.Matches[0].ExcerptContent)
				require.False(t, seen, "considered bodies and serialized skeletons cannot publish delivery")
			}
			cfg.BudgetCharsOverride = budgets[1]
			data, err = semanticPublicationCall(context.Background(), cfg, "delivery")
			larger := decodeSemanticPublication(t, data, err)
			freshData, err := semanticPublicationCall(context.Background(), cfg, "fresh")
			fresh := decodeSemanticPublication(t, freshData, err)
			require.Len(t, larger.Matches, 1)
			if budgets[0] == 12000 {
				require.True(t, larger.Matches[0].ContentDeduped)
				require.Empty(t, larger.Matches[0].FullFileContent)
				require.NotContains(t, larger.Text, "source evidence for the selected function")
			} else {
				require.Equal(t, compactSourceBody(fresh.Matches[0]), compactSourceBody(larger.Matches[0]))
				body := compactSourceBody(larger.Matches[0])
				require.NotEmpty(t, body.Content)
				fp, seen, err = cfg.IntelStore.SessionItemFingerprint(context.Background(), "delivery", "code:pkg/service.go")
				require.NoError(t, err)
				require.True(t, seen)
				require.Equal(t, fingerprintText(body.Content), fp)
			}
			require.Equal(t, first.Matches[0].Path, larger.Matches[0].Path)
			require.Equal(t, first.Matches[0].Score, larger.Matches[0].Score)
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
		require.Len(t, out.Matches, 1)
		match := out.Matches[0]
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
	require.Len(t, control.Matches, 2)
	require.Contains(t, control.Text, "UNIQUE_FINAL_EVIDENCE")
	require.Contains(t, control.Text, "SECOND_UNIQUE_Δ界")
	// Deny the second ranked source so suppression must retain the preceding
	// answer text, group header, and complete source without shifting its bytes.
	blocked := control.Matches[1]
	cfg.SessionStore = &competingSemanticReservationStore{SessionDedupeStore: cfg.IntelStore, blocked: semdb.SessionItem{Key: semanticMatchKey(blocked), Fingerprint: fingerprintText(compactSourceBody(blocked).Content)}}
	data, err = semanticPublicationCall(context.Background(), cfg, "grouped", "pkg.Service.Run", "pkg.Other.Run")
	out := decodeSemanticPublication(t, data, err)
	require.Equal(t, semanticMatchPaths(control.Matches), semanticMatchPaths(out.Matches))
	require.True(t, utf8.ValidString(out.Text))
	for i, match := range out.Matches {
		require.Equal(t, control.Matches[i].Score, match.Score)
		marker := "UNIQUE_FINAL_EVIDENCE"
		if match.Path == "other/service.go" {
			marker = "SECOND_UNIQUE_Δ界"
		}
		if i == 1 {
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
