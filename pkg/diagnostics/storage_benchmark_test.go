package diagnostics

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These files model cleanly closed short CLI sessions. The same fixture is
// used before and after admission changes, with no caller-side retry.
func seedRetainedSegments(tb testing.TB, root string, count int) {
	tb.Helper()
	dir := filepath.Join(root, ".rhizome", "diagnostics", "events")
	if err := ensureDirectories(filepath.Dir(dir)); err != nil {
		tb.Fatal(err)
	}
	data := []byte(`{"schemaVersion":1,"name":"synthetic.history"}` + "\n")
	for i := 0; i < count; i++ {
		path := filepath.Join(dir, fmt.Sprintf("20261004-synthetic-%06d-0.jsonl", i))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			tb.Fatal(err)
		}
	}
}

func BenchmarkRetainedHistoryAdmission(b *testing.B) {
	for _, count := range []int{2300, 4000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			root := b.TempDir()
			seedRetainedSegments(b, root, count)
			r, err := Open(root, Options{Stderr: io.Discard})
			if err != nil {
				b.Fatal(err)
			}
			defer r.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := r.withGuard(func() error { return r.admitLocked(64<<10, 1, time.Now()) }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRetainedHistoryReport(b *testing.B) {
	root := b.TempDir()
	seedRetainedSegments(b, root, 4000)
	r, err := Open(root, Options{Stderr: io.Discard})
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := WithOperation(context.Background(), NewOperation("search", "benchmark"))
		if err := r.PublishReport(ctx, Report{Status: "success"}); err != nil {
			b.Fatal(err)
		}
	}
}
