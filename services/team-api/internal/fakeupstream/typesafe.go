package fakeupstream

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/atomicobject/rhizome/pkg/typesafe"
)

// TypeSafe routes, matching TypeSafe's API.
const (
	TypeSafeEvalPath   = "/v1/systemone"
	TypeSafeModelsPath = "/v1/models"
)

// TypeSafe is a fake TypeSafe System One API. Its answers are deterministic
// functions of the request body and pass pkg/typesafe's validation.
type TypeSafe struct {
	fake
	seq int
}

func NewTypeSafe(opts Options) *TypeSafe { return &TypeSafe{fake: newFake(opts)} }

func (t *TypeSafe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if t.serveStats(w, r) {
		return
	}
	switch r.URL.Path {
	case TypeSafeModelsPath:
		if !route(w, r, http.MethodGet, TypeSafeModelsPath) {
			return
		}
		t.setRequestID(w)
		if _, ok := t.begin(w, r); ok {
			writeJSON(w, http.StatusOK, map[string]any{"models": []map[string]string{{
				"name": typesafe.DefaultModel, "description": "Fake Jev for local development", "release_date": "2026-01-01",
			}}})
		}
	default:
		if route(w, r, http.MethodPost, TypeSafeEvalPath) {
			t.evaluate(w, r)
		}
	}
}

func (t *TypeSafe) setRequestID(w http.ResponseWriter) {
	t.mu.Lock()
	t.seq++
	id := "fake-ts-" + strconv.Itoa(t.seq)
	t.mu.Unlock()
	w.Header().Set("x-typesafe-request-id", id)
}

func (t *TypeSafe) evaluate(w http.ResponseWriter, r *http.Request) {
	t.setRequestID(w)
	body, ok := t.begin(w, r)
	if !ok {
		return
	}
	var req typesafe.Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	model := req.Model
	if model == "" {
		model = typesafe.DefaultModel
	}
	answers := make(map[string]any, len(req.Questions))
	for id, q := range req.Questions {
		seed := sha256.Sum256(append([]byte(id+"\x00"), body...))
		switch q := q.(type) {
		case *typesafe.Choice:
			options := make([]string, 0, len(q.Criteria))
			for name := range q.Criteria {
				options = append(options, name)
			}
			sort.Strings(options)
			probs, best := distribution(options, seed)
			answers[id] = map[string]any{"type": "choice", "choice": options[best], "probabilities": probs, "confidence": probs[options[best]]}
		case *typesafe.Score:
			levels := make([]string, len(q.Criteria))
			legend := make(map[string]any, len(q.Criteria))
			for i, description := range q.Criteria {
				levels[i] = strconv.Itoa(i)
				legend[levels[i]] = description
			}
			probs, best := distribution(levels, seed)
			score := 0.0
			for i, level := range levels {
				score += float64(i) * probs[level]
			}
			answers[id] = map[string]any{"type": "score", "score": score, "probabilities": probs, "legend": legend, "confidence": probs[levels[best]]}
		case *typesafe.Noul:
			answers[id] = map[string]any{"type": "noul", "noul": float64(seed[0]) / 255}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"model":   model,
		"answers": answers,
		"usage":   map[string]int{"input_tokens": tokens(string(body)), "output_tokens": len(answers)},
	})
}

// distribution spreads probability over keys using seed bytes as weights and
// returns it with the index of the most likely key.
func distribution(keys []string, seed [32]byte) (map[string]float64, int) {
	weights := make([]float64, len(keys))
	sum, best := 0.0, 0
	for i := range keys {
		weights[i] = 1 + float64(seed[i%len(seed)]^byte(i/len(seed)))
		sum += weights[i]
		if weights[i] > weights[best] {
			best = i
		}
	}
	probs := make(map[string]float64, len(keys))
	for i, k := range keys {
		probs[k] = weights[i] / sum
	}
	return probs, best
}
