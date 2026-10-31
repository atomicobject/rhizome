package wire

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The server depends on this package, so it must stay standard-library only.
// Standard-library imports have no dot in their first path element.
func TestImportsOnlyStandardLibrary(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if first, _, _ := strings.Cut(path, "/"); strings.Contains(first, ".") || path == "C" {
				t.Errorf("%s imports %s; wire must use only the standard library", file, path)
			}
		}
	}
}

// Rhizome packs a Voyage batch to at most 1,000 inputs and 900,000 estimated
// tokens, estimating one token per four bytes (pkg/embeddings). That caps raw
// text at 3,600,000 bytes. Go's encoder writes a control character, <, >, or &
// as a six-byte \u escape, so text made only of those bytes is the worst case.
func TestVoyageRequestCeilingCoversLargestRhizomeBatch(t *testing.T) {
	const rawBytes = 900_000 * 4
	input := make([]string, MaxVoyageInputs)
	for i := range input {
		input[i] = strings.Repeat("<\x01", rawBytes/MaxVoyageInputs/2)
	}
	body, err := json.Marshal(map[string]any{
		"model": "voyage-4-lite", "input": input,
		"input_type": "document", "output_dimension": MaxVoyageOutputDimension,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("worst-case escaped request: %d bytes (%.1f MiB) of a %d MiB ceiling", len(body), float64(len(body))/(1<<20), MaxVoyageRequestBytes>>20)
	if len(body) > MaxVoyageRequestBytes {
		t.Fatalf("worst-case request %d bytes exceeds MaxVoyageRequestBytes %d", len(body), MaxVoyageRequestBytes)
	}
}

// A float32 needs at most 25 bytes as a JSON number with its separator, for
// example "-1.2345678901234567e-05," from a float64 rendering. At the maximum
// batch and dimension the response must still fit the ceiling. At 2,048
// dimensions it would not, which is why MaxVoyageOutputDimension is 1,024.
func TestVoyageResponseCeilingCoversLargestVectors(t *testing.T) {
	const perFloat = len("-1.2345678901234567e-05,")
	const perItem = len(`{"object":"embedding","embedding":[],"index":1000},`)
	size := MaxVoyageInputs*(MaxVoyageOutputDimension*perFloat+perItem) + 1024
	t.Logf("worst-case response: %d bytes (%.1f MiB) of a %d MiB ceiling", size, float64(size)/(1<<20), MaxResponseBytes>>20)
	if size > MaxResponseBytes {
		t.Fatalf("worst-case response %d bytes exceeds MaxResponseBytes %d", size, MaxResponseBytes)
	}
}
