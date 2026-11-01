package fakeupstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/typesafe"
)

const testKey = "synthetic-test-key"

func serve(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func post(t *testing.T, url, key, body string) (*http.Response, []byte, error) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data, nil
}

func tsClient(t *testing.T, base string) *typesafe.Client {
	t.Helper()
	c, err := typesafe.NewClient(testKey, typesafe.WithBaseURL(base), typesafe.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTypeSafeClientAcceptsEveryQuestionKind(t *testing.T) {
	fake := NewTypeSafe(Options{Key: testKey})
	c := tsClient(t, serve(t, fake))
	ctx := context.Background()

	choice, err := c.Choose(ctx, "a note about dogs", typesafe.Choice{Criteria: map[string]any{"animals": "about animals", "plants": nil, "rocks": map[string]any{"kind": "geology"}}})
	if err != nil || choice.RequestID == "" || choice.Model != typesafe.DefaultModel {
		t.Fatalf("choose: %+v %v", choice, err)
	}
	score, err := c.Score(ctx, map[string]any{"title": "x"}, typesafe.Score{Criteria: []any{"bad", "ok", "good"}})
	if err != nil || score.Answer.Score < 0 || score.Answer.Score > 2 {
		t.Fatalf("score: %+v %v", score, err)
	}
	noul, err := c.Check(ctx, []any{"a", "b"}, typesafe.Noul{Instructions: "is it?"})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	mixed, err := c.Evaluate(ctx, typesafe.Request{State: "s", Questions: map[string]typesafe.Question{
		"c": typesafe.Choice{Criteria: map[string]any{"x": nil, "y": nil}},
		"s": typesafe.Score{Criteria: []any{"0", "1"}},
		"n": typesafe.Noul{},
	}})
	if err != nil || len(mixed.Answers) != 3 || mixed.Usage.InputTokens <= 0 {
		t.Fatalf("evaluate: %+v %v", mixed, err)
	}
	// Deterministic: the same request gives the same answer.
	again, _ := c.Check(ctx, []any{"a", "b"}, typesafe.Noul{Instructions: "is it?"})
	if again.Answer != noul.Answer {
		t.Fatalf("noul not deterministic: %v vs %v", noul.Answer, again.Answer)
	}
	models, err := c.Models(ctx)
	if err != nil || len(models.Models) == 0 || models.RequestID == "" {
		t.Fatalf("models: %+v %v", models, err)
	}
	if got := fake.Stats(); got[TypeSafeEvalPath] != 5 || got[TypeSafeModelsPath] != 1 {
		t.Fatalf("stats = %v", got)
	}
}

func TestVoyageShapeAndDeterminism(t *testing.T) {
	url := serve(t, NewVoyage(Options{Key: testKey})) + VoyagePath
	embed := func(body string) map[string]any {
		resp, data, err := post(t, url, testKey, body)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("status %v %s %v", resp, data, err)
		}
		var out map[string]any
		_ = json.Unmarshal(data, &out)
		return out
	}
	out := embed(`{"model":"voyage-4","input":["hello","abcdefghi"],"input_type":"document"}`)
	if out["object"] != "list" || out["model"] != "voyage-4" || out["usage"].(map[string]any)["total_tokens"] != 5.0 {
		t.Fatalf("response = %v", out)
	}
	data := out["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("data = %d", len(data))
	}
	first := data[0].(map[string]any)
	vec := first["embedding"].([]any)
	if first["object"] != "embedding" || first["index"] != 0.0 || len(vec) != 1024 {
		t.Fatalf("item = %v (len %d)", first["object"], len(vec))
	}
	var norm float64
	for _, x := range vec {
		norm += x.(float64) * x.(float64)
	}
	if math.Abs(norm-1) > 1e-4 {
		t.Fatalf("norm = %v", norm)
	}
	small := embed(`{"model":"voyage-4","input":["hello"],"output_dimension":256}`)
	if got := small["data"].([]any)[0].(map[string]any)["embedding"].([]any); len(got) != 256 {
		t.Fatalf("dims = %d", len(got))
	}
	if !reflect.DeepEqual(Vector("hello", 64), Vector("hello", 64)) || reflect.DeepEqual(Vector("hello", 64), Vector("hellp", 64)) {
		t.Fatal("vectors are not deterministic per input")
	}
}

func TestAuthRequired(t *testing.T) {
	for name, h := range map[string]http.Handler{"voyage": NewVoyage(Options{Key: testKey}), "typesafe": NewTypeSafe(Options{Key: testKey})} {
		path := VoyagePath
		if name == "typesafe" {
			path = TypeSafeEvalPath
		}
		base := serve(t, h)
		for _, key := range []string{"", "wrong"} {
			resp, _, err := post(t, base+path, key, `{"model":"m","input":["x"]}`)
			if err != nil || resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("%s key %q: %v %v", name, key, resp, err)
			}
		}
	}
	c, _ := typesafe.NewClient("wrong", typesafe.WithBaseURL(serve(t, NewTypeSafe(Options{Key: testKey}))))
	var apiErr *typesafe.APIError
	if _, err := c.Models(context.Background()); !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Fatalf("models with wrong key: %v", err)
	}
}

func TestFaultMarkers(t *testing.T) {
	voyage := NewVoyage(Options{Key: testKey})
	vURL := serve(t, voyage) + VoyagePath
	typeSafe := NewTypeSafe(Options{Key: testKey})
	tsURL := serve(t, typeSafe) + TypeSafeEvalPath
	body := func(marker string) string { return `{"model":"m","input":["a","b ` + marker + `"]}` }

	t.Run("status", func(t *testing.T) {
		resp, _, err := post(t, vURL, testKey, body("__fault:status:503__"))
		if err != nil || resp.StatusCode != 503 {
			t.Fatalf("%v %v", resp, err)
		}
	})
	t.Run("retry_after", func(t *testing.T) {
		resp, _, err := post(t, vURL, testKey, body("__fault:retry_after:7__"))
		if err != nil || resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "7" {
			t.Fatalf("%v %v", resp, err)
		}
	})
	t.Run("slow", func(t *testing.T) {
		start := time.Now()
		resp, _, err := post(t, vURL, testKey, body("__fault:slow:150__"))
		if err != nil || resp.StatusCode != 200 || time.Since(start) < 150*time.Millisecond {
			t.Fatalf("%v %v after %v", resp, err, time.Since(start))
		}
	})
	t.Run("drop", func(t *testing.T) {
		before := voyage.Stats()[VoyagePath]
		if _, _, err := post(t, vURL, testKey, body("__fault:drop__")); err == nil {
			t.Fatal("dropped request got a response")
		}
		if got := voyage.Stats()[VoyagePath]; got != before+1 {
			t.Fatalf("drop counted %d times", got-before)
		}
	})
	t.Run("typesafe body", func(t *testing.T) {
		c := tsClient(t, strings.TrimSuffix(tsURL, TypeSafeEvalPath))
		_, err := c.Check(context.Background(), "please __fault:status:500__", typesafe.Noul{})
		var apiErr *typesafe.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != 500 || apiErr.RequestID == "" {
			t.Fatalf("status fault: %v", err)
		}
		if _, err := c.Check(context.Background(), "please __fault:drop__", typesafe.Noul{}); err == nil || errors.As(err, &apiErr) {
			t.Fatalf("drop fault should be a transport error: %v", err)
		}
	})
	t.Run("stats endpoint", func(t *testing.T) {
		resp, err := http.Get(strings.TrimSuffix(vURL, VoyagePath) + StatsPath)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var got map[string]int
		_ = json.NewDecoder(resp.Body).Decode(&got)
		if !reflect.DeepEqual(got, voyage.Stats()) || got[VoyagePath] != 4 {
			t.Fatalf("stats = %v, want %v", got, voyage.Stats())
		}
	})
}

func TestDelayOption(t *testing.T) {
	url := serve(t, NewVoyage(Options{Key: testKey, Delay: 100 * time.Millisecond})) + VoyagePath
	start := time.Now()
	resp, _, err := post(t, url, testKey, `{"model":"m","input":"one"}`)
	if err != nil || resp.StatusCode != 200 || time.Since(start) < 100*time.Millisecond {
		t.Fatalf("%v %v", resp, err)
	}
}
