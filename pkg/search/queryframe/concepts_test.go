package queryframe

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedConceptMatcherKeepsLongQueryCoverageIndependent(t *testing.T) {
	terms := make([]string, 70)
	for index := range terms {
		terms[index] = fmt.Sprintf("concept%02d", index)
	}
	text := strings.Join(terms, " ")
	matcher := Extract(text).ConceptMatcher()
	type result struct{ full, one, none float64 }
	results := make(chan result, 8)
	for range 8 {
		go func() {
			results <- result{matcher.Coverage(text), matcher.Coverage("concept69"), matcher.Coverage("unrelated")}
		}()
	}
	for range 8 {
		got := <-results
		require.Equal(t, 1.0, got.full)
		require.Equal(t, 1.0/70, got.one)
		require.Zero(t, got.none)
	}
}
