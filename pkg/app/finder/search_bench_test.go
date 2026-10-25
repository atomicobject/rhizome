package finder

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/store"
)

var benchWords = strings.Fields(`schema roadmap effort spec review design finder index search
note graph ontology runtime desktop toolbar theme palette migration release quality
gate decision record meeting sync planning customer onboarding billing invoice account
report weekly retro incident outage analysis metrics latency cache worker queue writer`)

// syntheticCatalog builds a deterministic 20,000-note vault plus code files.
func syntheticCatalog(notes, code int) *Catalog {
	r := rand.New(rand.NewSource(1))
	word := func() string { return benchWords[r.Intn(len(benchWords))] }
	types := []string{"", "Effort", "Spec", "Decision", "Meeting"}
	noteRows := make([]store.FinderNoteRow, notes)
	for i := range noteRows {
		title := fmt.Sprintf("%s %s %s %d", strings.ToUpper(word()[:1])+word()[1:], word(), word(), i)
		noteRows[i] = store.FinderNoteRow{
			Path:  fmt.Sprintf("docs/%s/%s/%s-%s-%d.md", word(), word(), word(), word(), i),
			Title: title, Type: types[i%len(types)], Mtime: int64(i),
		}
	}
	codeRows := make([]store.FinderCodeRow, code)
	for i := range codeRows {
		codeRows[i] = store.FinderCodeRow{Path: fmt.Sprintf("pkg/%s/%s/%s_%s_%d.go", word(), word(), word(), word(), i), Lang: "go"}
	}
	return NewCatalog(noteRows, codeRows)
}

var benchQueries = []string{
	"s", "sc", "sch", "schema", "shcema", "roadmap", "rodmap", "eff spec", "finder index",
	"docs/review", "pkg/cache", "Weekly", "MeetingSync", "incident analysis 42", "qzx",
	"latncy", "planing", "ontology runtime 19999", "writer.go", "review_design",
}

// BenchmarkSearch20kNotes reports p50 and p95 per-query latency, typo
// fallback included, over a 20,000-note plus 8,000-code-file catalog.
func BenchmarkSearch20kNotes(b *testing.B) {
	c := syntheticCatalog(20000, 8000)
	now := time.Now()
	opens := map[string]Open{}
	for i := 0; i < 500; i++ {
		opens[c.entries[i*37].Path] = Open{Count: i%20 + 1, LastOpened: now.Add(-time.Duration(i) * time.Hour)}
	}
	var samples []time.Duration
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := benchQueries[i%len(benchQueries)]
		start := time.Now()
		c.Search(Query{Text: q, Limit: DefaultLimit, Opens: opens, Now: now})
		samples = append(samples, time.Since(start))
	}
	b.StopTimer()
	slices.Sort(samples)
	pct := func(p float64) float64 {
		return float64(samples[int(p*float64(len(samples)-1))].Microseconds()) / 1000
	}
	b.ReportMetric(pct(0.50), "p50-ms")
	b.ReportMetric(pct(0.95), "p95-ms")
	b.ReportMetric(pct(1), "max-ms")
}
