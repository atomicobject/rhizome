package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkValidationReads(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("files_%d", count), func(b *testing.B) {
			ctx := context.Background()
			store, err := Open(filepath.Join(b.TempDir(), "validation.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			generation, err := store.SetValidationRunning(ctx)
			if err != nil {
				b.Fatal(err)
			}
			if _, err = store.PublishValidationSnapshot(ctx, validationStoreFixture(generation, count)); err != nil {
				b.Fatal(err)
			}
			b.Run("page100", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					page, err := store.GetValidationDiagnosticsPage(ctx, ValidationDiagnosticPageRequest{Generation: generation, Limit: 100})
					if err != nil || len(page.Diagnostics) != 100 {
						b.Fatalf("page: %v", err)
					}
				}
			})
			scopes := make([]ValidationScope, 50)
			for i := range scopes {
				scopes[i] = ValidationScope{Kind: ValidationScopeFile, Key: fmt.Sprintf("notes/%04d.md", i)}
			}
			b.Run("summaries50", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					result, err := store.GetValidationScopeSummaries(ctx, ValidationScopeSummaryRequest{Generation: generation, Scopes: scopes})
					if err != nil || len(result.Summaries) != 50 {
						b.Fatalf("summaries: %v", err)
					}
				}
			})
		})
	}
}

func BenchmarkValidationPublication(b *testing.B) {
	for _, count := range []int{1000, 10000} {
		b.Run(fmt.Sprintf("files_%d", count), func(b *testing.B) {
			ctx := context.Background()
			store, err := Open(filepath.Join(b.TempDir(), "publication.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			snapshot := validationStoreFixture(1, count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				snapshot.Generation, err = store.SetValidationRunning(ctx)
				if err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if _, err = store.PublishValidationSnapshot(ctx, snapshot); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
