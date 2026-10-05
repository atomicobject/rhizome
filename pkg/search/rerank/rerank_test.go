package rerank

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
)

func TestVoyageRerankerSendsDocumentsAndMapsScoresByIndex(t *testing.T) {
	var got voyageRequest
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		// Voyage returns data sorted by score, not input order.
		_, _ = io.WriteString(w, `{"object":"list","data":[{"relevance_score":0.9,"index":1},{"relevance_score":0.25,"index":0}],"model":"rerank-2.5-lite","usage":{"total_tokens":8}}`)
	}))
	defer server.Close()

	r := &VoyageReranker{APIKey: "secret", Endpoint: server.URL}
	scores, err := r.Rerank(context.Background(), "how does ranking work", []Document{{ID: "a", Text: "alpha"}, {ID: "b", Text: "beta"}})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if auth != "Bearer secret" {
		t.Errorf("Authorization = %q", auth)
	}
	if got.Query != "how does ranking work" || got.Model != DefaultVoyageModel || got.TopK != 2 || !got.Truncation {
		t.Errorf("request = %+v", got)
	}
	if len(got.Documents) != 2 || got.Documents[0] != "alpha" || got.Documents[1] != "beta" {
		t.Errorf("documents = %v", got.Documents)
	}
	if len(scores) != 2 || scores[0] != 0.25 || scores[1] != 0.9 {
		t.Errorf("scores = %v, want input order [0.25 0.9]", scores)
	}
}

func TestVoyageRerankerReportsErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"detail":"rate limited"}`)
	}))
	defer server.Close()

	r := &VoyageReranker{APIKey: "secret", Endpoint: server.URL}
	if _, err := r.Rerank(context.Background(), "q", []Document{{Text: "alpha"}}); err == nil {
		t.Fatal("expected an error for a non-200 response")
	}
}

func TestCachedRerankerSkipsProviderOnHit(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req voyageRequest
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if calls == 2 && (len(req.Documents) != 1 || req.Documents[0] != "gamma") {
			t.Errorf("second call documents = %v, want only the miss", req.Documents)
		}
		out := struct {
			Data []map[string]any `json:"data"`
		}{}
		for i := range req.Documents {
			score := map[string]float64{"alpha": .2, "beta": .8, "gamma": .4}[req.Documents[i]]
			out.Data = append(out.Data, map[string]any{"index": i, "relevance_score": score})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer server.Close()

	cfg := ProviderConfig{Provider: "voyage", Model: DefaultVoyageModel}
	inner := &VoyageReranker{APIKey: "secret", Endpoint: server.URL}
	cache, err := NewCachedReranker(inner, cfg, filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("NewCachedReranker: %v", err)
	}
	defer func() { _ = cache.Close() }()

	docs := []Document{{ID: "a", Text: "alpha"}, {ID: "b", Text: "beta"}}
	if _, err := cache.Rerank(context.Background(), "q", docs); err != nil {
		t.Fatalf("first Rerank: %v", err)
	}
	scores, err := cache.Rerank(context.Background(), "q", docs)
	if err != nil {
		t.Fatalf("second Rerank: %v", err)
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (second run fully cached)", calls)
	}
	if !reflect.DeepEqual(scores, []float64{.2, .8}) {
		t.Errorf("cached scores = %v", scores)
	}

	// A new document is the only text sent on the next call.
	scores, err = cache.Rerank(context.Background(), "q", []Document{{ID: "b", Text: "beta"}, {ID: "c", Text: "gamma"}, {ID: "a", Text: "alpha"}})
	if err != nil {
		t.Fatalf("third Rerank: %v", err)
	}
	if !reflect.DeepEqual(scores, []float64{.8, .4, .2}) {
		t.Errorf("mixed cached scores = %v", scores)
	}
	if calls != 2 {
		t.Fatalf("provider calls = %d, want 2", calls)
	}
}

func TestNewFromEnvDisabledWithoutProvider(t *testing.T) {
	t.Setenv("RHIZOME_RERANK_PROVIDER", "")
	if _, _, ok := NewFromEnv(); ok {
		t.Error("expected reranking to be disabled when no provider is configured")
	}
	t.Setenv("RHIZOME_RERANK_PROVIDER", "cohere")
	if _, _, ok := NewFromEnv(); ok {
		t.Error("expected an unsupported provider to disable reranking")
	}
}
