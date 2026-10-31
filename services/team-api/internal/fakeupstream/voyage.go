package fakeupstream

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
)

// VoyagePath is the embeddings route, matching Voyage's API.
const VoyagePath = "/v1/embeddings"

const defaultDimension = 1024

// Voyage is a fake Voyage AI embeddings API.
type Voyage struct{ fake }

func NewVoyage(opts Options) *Voyage { return &Voyage{newFake(opts)} }

func (v *Voyage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if v.serveStats(w, r) || !route(w, r, http.MethodPost, VoyagePath) {
		return
	}
	body, ok := v.begin(w, r)
	if !ok {
		return
	}
	var req struct {
		Input           json.RawMessage `json:"input"`
		Model           string          `json:"model"`
		OutputDimension *int            `json:"output_dimension"`
	}
	if json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "request body must be a JSON object")
		return
	}
	var inputs []string
	if json.Unmarshal(req.Input, &inputs) != nil {
		var one string
		if json.Unmarshal(req.Input, &one) != nil {
			writeError(w, http.StatusBadRequest, "input must be a string or a list of strings")
			return
		}
		inputs = []string{one}
	}
	dims := defaultDimension
	if req.OutputDimension != nil {
		dims = *req.OutputDimension
	}
	if len(inputs) == 0 || req.Model == "" || dims <= 0 || dims > 4096 {
		writeError(w, http.StatusBadRequest, "model, a non-empty input, and a positive output_dimension are required")
		return
	}
	type item struct {
		Object    string    `json:"object"`
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	}
	data := make([]item, len(inputs))
	total := 0
	for i, s := range inputs {
		data[i] = item{"embedding", Vector(s, dims), i}
		total += tokens(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
		"model":  req.Model,
		"usage":  map[string]int{"total_tokens": total},
	})
}

// Vector is the fake's deterministic unit-length embedding of text: SHA-256
// of the text and a block counter, expanded to dims values in [-1, 1].
func Vector(text string, dims int) []float32 {
	raw := make([]float64, dims)
	var norm float64
	var block [sha256.Size]byte
	for i := range raw {
		if i%8 == 0 {
			var counter [4]byte
			binary.BigEndian.PutUint32(counter[:], uint32(i/8))
			block = sha256.Sum256(append([]byte(text+"\x00"), counter[:]...))
		}
		u := binary.BigEndian.Uint32(block[(i%8)*4:])
		raw[i] = float64(u)/math.MaxUint32*2 - 1
		norm += raw[i] * raw[i]
	}
	norm = math.Sqrt(norm)
	out := make([]float32, dims)
	for i, x := range raw {
		out[i] = float32(x / norm)
	}
	return out
}
