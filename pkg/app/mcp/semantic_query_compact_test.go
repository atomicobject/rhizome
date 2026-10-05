package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/answer"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/testutil/sqlitefixture"
	"github.com/stretchr/testify/require"
)

func TestCompactSemanticQueryResponseOwnsBodyOnceAndResolvesRoles(t *testing.T) {
	resp := semanticQueryResponse{
		Text: "rendered source body",
		Matches: []SemanticMatchPayload{{
			Type:             "code",
			Path:             "pkg/service.go",
			Title:            "Service",
			FullFileContent:  "rendered source body",
			ContentKind:      string(planFull),
			ContentTruncated: true,
			ContentDeduped:   true,
			Included:         true,
			Evidence:         map[string]float64{"code_vector": 0.8},
		}},
		MustRead:   []answer.Item{{Type: "code", Path: "pkg/service.go"}},
		Supporting: []answer.Item{{Type: "note", Path: "missing.md"}},
	}

	got := compactSemanticQueryResponse(resp)

	require.Empty(t, got.Text)
	require.Empty(t, got.Matches)
	require.Empty(t, got.MustRead)
	require.Empty(t, got.Supporting)
	require.NotNil(t, got.Compact)
	require.Len(t, got.Compact.Sources, 1)
	source := got.Compact.Sources[0]
	require.Equal(t, "code:pkg/service.go", source.Ref)
	require.Equal(t, resp.Matches[0].Evidence, source.Evidence)
	source.Evidence["code_vector"] = 0.4
	require.Equal(t, 0.8, resp.Matches[0].Evidence["code_vector"], "projection must not alias explain evidence")
	require.Equal(t, "rendered source body", source.Body.Content)
	require.Equal(t, "full", source.Body.Kind)
	require.True(t, source.Body.Truncated)
	require.True(t, source.Body.Deduped)
	require.Equal(t, []string{source.Ref}, got.Compact.Roles.MustRead)
	require.Empty(t, got.Compact.Roles.Supporting)
}

func TestCompactSemanticQueryResponseClearsNoResultDetailedFields(t *testing.T) {
	got := compactSemanticQueryResponse(semanticQueryResponse{
		Text:     "No more results.",
		Warnings: []search.Warning{{Code: "scope-unavailable", Message: "No scoped source is available."}},
	})

	require.Empty(t, got.Text)
	require.Empty(t, got.Matches)
	require.Empty(t, got.MustRead)
	require.Empty(t, got.Supporting)
	require.NotNil(t, got.Compact)
	require.Empty(t, got.Compact.Sources)
	require.Equal(t, "scope-unavailable", got.Warnings[0].Code)
}

func TestCompactSemanticQueryResponseDeduplicatesSourceBodies(t *testing.T) {
	resp := semanticQueryResponse{
		Matches: []SemanticMatchPayload{
			{Type: "code", Path: "pkg/service.go", Title: "Service"},
			{
				Type:             "code",
				Path:             "pkg/service.go",
				Title:            "Service",
				ExcerptContent:   "func Service() {}",
				ContentKind:      string(planExcerpt),
				ContentTruncated: true,
			},
		},
		MustRead: []answer.Item{{Type: "code", Path: "pkg/service.go"}},
	}

	got := compactSemanticQueryResponse(resp)

	require.Len(t, got.Compact.Sources, 1)
	require.NotNil(t, got.Compact.Sources[0].Body)
	require.Equal(t, "func Service() {}", got.Compact.Sources[0].Body.Content)
	require.Equal(t, []string{"code:pkg/service.go"}, got.Compact.Roles.MustRead)

	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.Equal(t, 1, bytes.Count(encoded, []byte("func Service() {}")))
}

func TestMarshalCompactSemanticQueryResponsePreservesCompactShapeWithinBudget(t *testing.T) {
	resp := compactSemanticQueryResponse(semanticQueryResponse{
		Query: "Service",
		Count: 1,
		Text:  "detailed source body",
		Matches: []SemanticMatchPayload{{
			Type:            "code",
			Path:            "pkg/service.go",
			FullFileContent: strings.Repeat("func Service() {}\n", 120),
			ContentKind:     string(planFull),
			Included:        true,
		}},
		MustRead: []answer.Item{{Type: "code", Path: "pkg/service.go"}},
	})

	encoded, err := marshalSemanticQueryResponse(resp, 500)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), 500)
	require.Equal(t, strings.Repeat("func Service() {}\n", 120), resp.Compact.Sources[0].Body.Content)

	var got semanticQueryResponse
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.NotNil(t, got.Compact)
	require.Empty(t, got.Text)
	require.Empty(t, got.Matches)
	require.Equal(t, []string{"code:pkg/service.go"}, got.Compact.Roles.MustRead)
	require.Zero(t, got.Compact.OmittedSources)
}

func TestMarshalCompactSemanticQueryResponseReportsOmittedSources(t *testing.T) {
	resp := semanticQueryResponse{
		Query: "Service",
		Count: 2,
		Compact: &SemanticCompactResponse{
			Sources: []SemanticCompactSource{
				{Ref: "code:pkg/first.go", Type: "code", Path: "pkg/first.go"},
				{Ref: "code:pkg/second.go", Type: "code", Path: "pkg/second.go"},
			},
			Roles: SemanticCompactRoles{MustRead: []string{"code:pkg/first.go", "code:pkg/second.go"}},
		},
	}

	encoded, err := marshalSemanticQueryResponse(resp, 180)
	require.NoError(t, err)

	var got semanticQueryResponse
	require.NoError(t, json.Unmarshal(encoded, &got))
	require.NotNil(t, got.Compact)
	require.Positive(t, got.Compact.OmittedSources)
	for _, ref := range got.Compact.Roles.MustRead {
		found := false
		for _, source := range got.Compact.Sources {
			if source.Ref == ref {
				found = true
				break
			}
		}
		require.True(t, found)
	}
}

func TestMarkRenderedCompactSemanticBodiesMarksOnlyEncodedSources(t *testing.T) {
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "session.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	resp := semanticQueryResponse{
		Query: "Service",
		Count: 2,
		Compact: &SemanticCompactResponse{Sources: []SemanticCompactSource{
			{Ref: "code:pkg/first.go", Type: "code", Body: &SemanticCompactSourceBody{Kind: string(planExcerpt), Content: strings.Repeat("first ", 80)}},
			{Ref: "code:pkg/second.go", Type: "code", Body: &SemanticCompactSourceBody{Kind: string(planExcerpt), Content: strings.Repeat("second ", 80)}},
		}},
	}
	// Controlled render: the first source's body is encoded, the second source
	// carries only a stub, and a third source was omitted by the budget. Only
	// the encoded body may be marked as delivered.
	stub := "matches; read if needed"
	partial := semanticQueryResponse{Query: resp.Query, Count: 3, Compact: &SemanticCompactResponse{
		Sources: []SemanticCompactSource{
			{Ref: "code:pkg/first.go", Type: "code", Body: &SemanticCompactSourceBody{Kind: string(planExcerpt), Content: strings.Repeat("first ", 20)}},
			{Ref: "code:pkg/stub.go", Type: "code", Body: &SemanticCompactSourceBody{Kind: string(planStub), Content: stub}},
		},
		OmittedSources: 1,
	}}
	encoded, err := json.Marshal(partial)
	require.NoError(t, err)
	tracker := &sessionTracker{ctx: context.Background(), store: store, sessionID: "session"}
	encoded, err = finalizeCompactSemanticQueryResponse(tracker, encoded, 5000)
	require.NoError(t, err)

	var rendered semanticQueryResponse
	require.NoError(t, json.Unmarshal(encoded, &rendered))
	require.Equal(t, 1, rendered.Compact.OmittedSources)
	require.Len(t, rendered.Compact.Sources, 2)
	require.Equal(t, strings.Repeat("first ", 20), rendered.Compact.Sources[0].Body.Content, "precondition: one body is emitted")
	require.True(t, tracker.Seen("code:pkg/first.go", fingerprintText(strings.Repeat("first ", 20))))
	require.False(t, tracker.Seen("code:pkg/stub.go", fingerprintText(stub)))
	require.False(t, tracker.Seen("code:pkg/second.go", fingerprintText(strings.Repeat("second ", 80))))

	for _, budget := range []int{500, 5000} {
		encoded, err = marshalSemanticQueryResponse(resp, budget)
		require.NoError(t, err)
		tracker = &sessionTracker{ctx: context.Background(), store: store, sessionID: fmt.Sprintf("budget-%d", budget)}
		encoded, err = finalizeCompactSemanticQueryResponse(tracker, encoded, budget)
		require.NoError(t, err)
		rendered = semanticQueryResponse{}
		require.NoError(t, json.Unmarshal(encoded, &rendered))
		require.Len(t, rendered.Compact.Sources, 2)
		for i, source := range rendered.Compact.Sources {
			require.NotEmpty(t, source.Body.Content)
			require.True(t, tracker.Seen(source.Ref, fingerprintText(source.Body.Content)))
			if budget == 500 {
				require.True(t, source.Body.Truncated)
				require.False(t, tracker.Seen(source.Ref, fingerprintText(resp.Compact.Sources[i].Body.Content)))
			} else {
				require.False(t, source.Body.Truncated)
			}
		}
	}
}

// This barrier forces the obsolete read-then-write approach to expose its race:
// every reader observes the old value before any reader can mark a body sent.
type synchronizedFingerprintStore struct {
	semdb.SessionDedupeStore
	readers sync.WaitGroup
}

func (s *synchronizedFingerprintStore) SessionItemFingerprint(ctx context.Context, sessionID, key string) (string, bool, error) {
	fingerprint, found, err := s.SessionDedupeStore.SessionItemFingerprint(ctx, sessionID, key)
	s.readers.Done()
	s.readers.Wait()
	return fingerprint, found, err
}
func TestConcurrentCompactFinalizationEmitsEachBodyOnce(t *testing.T) {
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "session.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	const count = 8
	synchronized := &synchronizedFingerprintStore{SessionDedupeStore: store}
	synchronized.readers.Add(count)
	response := semanticQueryResponse{Count: 1, Compact: &SemanticCompactResponse{Sources: []SemanticCompactSource{{Ref: "code:pkg/service.go", Type: "code", Included: true, Body: &SemanticCompactSourceBody{Kind: string(planFull), Content: "func Run() {}"}}}}}
	encoded, err := marshalSemanticQueryResponse(response, 5000)
	require.NoError(t, err)
	type result struct {
		data []byte
		err  error
	}
	results := make(chan result, count)
	start := make(chan struct{})
	for i := 0; i < count; i++ {
		go func() {
			<-start
			tracker := &sessionTracker{ctx: context.Background(), store: synchronized, sessionID: "concurrent"}
			data, err := finalizeCompactSemanticQueryResponse(tracker, encoded, 5000)
			results <- result{data: data, err: err}
		}()
	}
	close(start)
	emitted := 0
	for i := 0; i < count; i++ {
		result := <-results
		require.NoError(t, result.err)
		var got semanticQueryResponse
		require.NoError(t, json.Unmarshal(result.data, &got))
		require.Len(t, got.Compact.Sources, 1)
		body := got.Compact.Sources[0].Body
		if body.Content != "" {
			emitted++
		} else {
			require.True(t, body.Deduped)
		}
	}
	require.Equal(t, 1, emitted)
}

func TestCompactFinalizationReleasesReservationsWhenQualificationsExceedBudget(t *testing.T) {
	store, err := sqlitefixture.Open(filepath.Join(t.TempDir(), "session.db"))
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	tracker := &sessionTracker{ctx: context.Background(), store: store, sessionID: "budget-release"}
	response := semanticQueryResponse{Count: 2, Compact: &SemanticCompactResponse{Sources: []SemanticCompactSource{
		{Ref: "code:a", Type: "code", Body: &SemanticCompactSourceBody{Kind: string(planFull), Content: "x"}},
		{Ref: "code:b", Type: "code", Body: &SemanticCompactSourceBody{Kind: string(planFull), Content: "unseen body"}},
	}}}
	tracker.MarkSent("code:a", fingerprintText("x"))
	encoded, err := marshalSemanticQueryResponse(response, 5000)
	require.NoError(t, err)
	_, err = finalizeCompactSemanticQueryResponse(tracker, encoded, len(encoded))
	require.ErrorContains(t, err, "dedupe qualifications")
	require.False(t, tracker.Seen("code:b", fingerprintText("unseen body")))
	retried, err := finalizeCompactSemanticQueryResponse(tracker, encoded, 5000)
	require.NoError(t, err)
	var got semanticQueryResponse
	require.NoError(t, json.Unmarshal(retried, &got))
	require.True(t, got.Compact.Sources[0].Body.Deduped)
	require.Equal(t, "unseen body", got.Compact.Sources[1].Body.Content)
}
