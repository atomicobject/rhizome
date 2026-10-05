package typesafe_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/typesafe"
	"github.com/stretchr/testify/require"
)

const mixedResponse = `{
  "model":"jev-pinned", "usage":{"input_tokens":123,"output_tokens":42},
  "answers":{
    "route":{"type":"choice","choice":"keep","confidence":0.8,"probabilities":{"keep":0.9,"review":0.099}},
    "quality":{"type":"score","score":0.75,"confidence":0.6,"legend":{"0":{"label":"weak"},"1":"strong"},"probabilities":{"0":0.25,"1":0.75}},
    "supported":{"type":"noul","noul":0}
  }
}`

func mixedRequest() typesafe.Request {
	return typesafe.Request{
		State: struct {
			Title string `json:"title"`
			Count int    `json:"count"`
		}{"A native Go record", 2},
		Questions: map[string]typesafe.Question{
			"route":     typesafe.Choice{Instructions: "Which action?", Criteria: map[string]any{"keep": nil, "review": []string{"unclear", "incomplete"}}},
			"quality":   typesafe.Score{Instructions: map[string]any{"ask": "Rate quality"}, Criteria: []any{map[string]any{"label": "weak"}, "strong"}},
			"supported": typesafe.Noul{Instructions: "Is it supported?", Criteria: &typesafe.NoulCriteria{True: "Fully supported", False: map[string]any{"otherwise": true}}},
		},
	}
}

func newClient(t *testing.T, handler http.HandlerFunc, options ...typesafe.Option) *typesafe.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	options = append([]typesafe.Option{typesafe.WithBaseURL(server.URL)}, options...)
	client, err := typesafe.NewClient("test-key", options...)
	require.NoError(t, err)
	return client
}

func TestMixedBatchNativeTypes(t *testing.T) {
	var received map[string]any
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/systemone", r.URL.Path)
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("x-typesafe-request-id", "req-123")
		fmt.Fprint(w, mixedResponse)
	}, typesafe.WithModel("jev-default"))

	request := mixedRequest()
	request.Model = "jev-pinned"
	response, err := client.Evaluate(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "jev-pinned", received["model"])
	require.Equal(t, map[string]any{"title": "A native Go record", "count": float64(2)}, received["state"])
	questions := received["questions"].(map[string]any)
	require.Equal(t, "choice", questions["route"].(map[string]any)["type"])
	require.Equal(t, "score", questions["quality"].(map[string]any)["type"])
	require.Equal(t, "noul", questions["supported"].(map[string]any)["type"])
	require.Equal(t, map[string]any{"ask": "Rate quality"}, questions["quality"].(map[string]any)["instructions"])
	require.Equal(t, "req-123", response.RequestID)
	require.Equal(t, int64(123), response.Usage.InputTokens)
	choice, err := response.Choice("route")
	require.NoError(t, err)
	require.Equal(t, "keep", choice.Choice)
	require.Equal(t, 0.9, choice.Probabilities["keep"])
	score, err := response.Score("quality")
	require.NoError(t, err)
	require.Equal(t, 0.75, score.Score)
	require.Equal(t, map[string]any{"label": "weak"}, score.Legend["0"])
	noul, err := response.Noul("supported")
	require.NoError(t, err)
	require.Zero(t, noul.Noul)
	_, err = response.Choice("supported")
	require.Error(t, err)
	_, err = response.Score("missing")
	require.Error(t, err)
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	require.JSONEq(t, mixedResponse, string(encoded))
}

func TestSingleQuestionMethodsPreserveTypesAndMetadata(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Model     string `json:"model"`
			Questions map[string]struct {
				Type string `json:"type"`
			} `json:"questions"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, "jev-pinned", request.Model)
		answers := map[string]string{
			"choice": `{"type":"choice","choice":"a","probabilities":{"a":1,"b":0},"confidence":1}`,
			"score":  `{"type":"score","score":1,"probabilities":{"0":0,"1":1},"legend":{"0":"bad","1":"good"},"confidence":1}`,
			"noul":   `{"type":"noul","noul":1}`,
		}
		w.Header().Set("x-typesafe-request-id", "single")
		fmt.Fprintf(w, `{"model":"jev-pinned","usage":{"input_tokens":5,"output_tokens":2},"answers":{"answer":%s}}`, answers[request.Questions["answer"].Type])
	}, typesafe.WithModel("jev-pinned"))
	choice, err := client.Choose(context.Background(), "text", typesafe.Choice{Criteria: map[string]any{"a": nil, "b": nil}})
	require.NoError(t, err)
	require.Equal(t, "a", choice.Answer.Choice)
	require.Equal(t, "single", choice.RequestID)
	require.Equal(t, "jev-pinned", choice.Model)
	require.Equal(t, int64(5), choice.Usage.InputTokens)
	score, err := client.Score(context.Background(), []string{"text"}, typesafe.Score{Criteria: []any{"bad", "good"}})
	require.NoError(t, err)
	require.Equal(t, float64(1), score.Answer.Score)
	check, err := client.Check(context.Background(), map[string]string{"text": "yes"}, typesafe.Noul{Instructions: "True?"})
	require.NoError(t, err)
	require.Equal(t, float64(1), check.Answer.Noul)
}

func TestInvalidRequestsNeverReachServer(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request reached server") })
	var nilQuestion *typesafe.Choice
	tests := map[string]typesafe.Request{
		"nil state":           {Questions: map[string]typesafe.Question{"q": typesafe.Noul{}}},
		"scalar state":        {State: 4, Questions: map[string]typesafe.Question{"q": typesafe.Noul{}}},
		"no questions":        {State: "text"},
		"nil question":        {State: "text", Questions: map[string]typesafe.Question{"q": nil}},
		"typed nil question":  {State: "text", Questions: map[string]typesafe.Question{"q": nilQuestion}},
		"choice options":      {State: "text", Questions: map[string]typesafe.Question{"q": typesafe.Choice{}}},
		"score levels":        {State: "text", Questions: map[string]typesafe.Question{"q": typesafe.Score{Criteria: []any{"only"}}}},
		"invalid description": {State: "text", Questions: map[string]typesafe.Question{"q": typesafe.Score{Criteria: []any{false, "good"}}}},
		"invalid instruction": {State: "text", Questions: map[string]typesafe.Question{"q": typesafe.Noul{Instructions: 3}}},
		"not JSON":            {State: make(chan int), Questions: map[string]typesafe.Question{"q": typesafe.Noul{}}},
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := client.Evaluate(context.Background(), request)
			require.Error(t, err)
		})
	}
}

func TestMalformedAnswersAreNotUsableResults(t *testing.T) {
	tests := map[string]string{
		"missing answer":      `{}`,
		"missing probability": `{"answer":{"type":"noul"}}`,
		"null probability":    `{"answer":{"type":"noul","noul":null}}`,
		"out of range":        `{"answer":{"type":"noul","noul":1.2}}`,
		"wrong ID":            `{"different":{"type":"noul","noul":0.5}}`,
		"extra answer":        `{"answer":{"type":"noul","noul":0.5},"extra":{"type":"noul","noul":0}}`,
		"wrong kind":          `{"answer":{"type":"choice","choice":"a","confidence":1,"probabilities":{"a":1}}}`,
		"unknown kind":        `{"answer":{"type":"future","noul":0.5}}`,
	}
	for name, answers := range tests {
		t.Run(name, func(t *testing.T) {
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"model":"jev","usage":{"input_tokens":1,"output_tokens":1},"answers":%s}`, answers)
			})
			_, err := client.Check(context.Background(), "text", typesafe.Noul{Instructions: "True?"})
			var responseErr *typesafe.ResponseError
			require.ErrorAs(t, err, &responseErr)
		})
	}
}

func TestModelsAndConcurrentReuse(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/v1/models", r.URL.Path)
		fmt.Fprint(w, `{"models":[{"name":"jev-latest","description":"stable","release_date":"2026-09-01"}]}`)
	})
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			models, err := client.Models(context.Background())
			require.NoError(t, err)
			require.Equal(t, "jev-latest", models.Models[0].Name)
		}()
	}
	wg.Wait()
}

func TestBatchValidatesOptionsLevelsAndRequiredMetadata(t *testing.T) {
	tests := map[string]func(map[string]any){
		"missing model":       func(r map[string]any) { delete(r, "model") },
		"missing usage":       func(r map[string]any) { delete(r, "usage") },
		"negative usage":      func(r map[string]any) { r["usage"].(map[string]any)["input_tokens"] = -1 },
		"missing usage field": func(r map[string]any) { delete(r["usage"].(map[string]any), "output_tokens") },
		"missing confidence":  func(r map[string]any) { delete(answer(r, "route"), "confidence") },
		"invalid confidence":  func(r map[string]any) { answer(r, "route")["confidence"] = 2 },
		"missing choice":      func(r map[string]any) { delete(answer(r, "route"), "choice") },
		"invented choice":     func(r map[string]any) { answer(r, "route")["choice"] = "invented" },
		"missing option":      func(r map[string]any) { answer(r, "route")["probabilities"] = map[string]any{"keep": 1} },
		"invented option": func(r map[string]any) {
			answer(r, "route")["probabilities"] = map[string]any{"keep": 0.5, "invented": 0.5}
		},
		"null probability": func(r map[string]any) { answer(r, "route")["probabilities"] = map[string]any{"keep": 1, "review": nil} },
		"invalid probability sum": func(r map[string]any) {
			answer(r, "route")["probabilities"] = map[string]any{"keep": 1, "review": 1}
		},
		"missing score":       func(r map[string]any) { delete(answer(r, "quality"), "score") },
		"score beyond rubric": func(r map[string]any) { answer(r, "quality")["score"] = 2 },
		"missing legend":      func(r map[string]any) { delete(answer(r, "quality"), "legend") },
		"wrong legend levels": func(r map[string]any) { answer(r, "quality")["legend"] = map[string]any{"0": "weak", "2": "strong"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			var response map[string]any
			require.NoError(t, json.Unmarshal([]byte(mixedResponse), &response))
			mutate(response)
			client := newClient(t, func(w http.ResponseWriter, r *http.Request) { require.NoError(t, json.NewEncoder(w).Encode(response)) })
			_, err := client.Evaluate(context.Background(), mixedRequest())
			var responseErr *typesafe.ResponseError
			require.ErrorAs(t, err, &responseErr)
		})
	}
}

func answer(response map[string]any, id string) map[string]any {
	return response["answers"].(map[string]any)[id].(map[string]any)
}
