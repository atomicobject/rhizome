package views

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"testing"
)

func BenchmarkSortRows(b *testing.B) {
	for _, count := range []int{10, 2048} {
		for _, kind := range []string{"int", "real", "string", "datetime", "unindexed"} {
			for _, repeated := range []bool{false, true} {
				b.Run(fmt.Sprintf("rows=%d/%s/repeated=%t", count, kind, repeated), func(b *testing.B) {
					input := make([]TableRow, count)
					for i := range input {
						n := (i * 1597) % count
						value := fmt.Sprint(n)
						switch kind {
						case "string":
							value = fmt.Sprintf(" [[Item %04d|Label]] ", n)
						case "datetime":
							value = fmt.Sprintf("2026-09-%02dT%02d:30:15.123-04:00", n%28+1, n%24)
						}
						var raw any = value
						if repeated {
							raw = []string{value, "invalid", value}
						}
						if i%17 == 0 {
							raw = nil
						}
						input[i] = TableRow{Ref: ontology.NodeRef{NotePath: fmt.Sprintf("row-%04d.md", i)}, Fields: map[string]any{"value": raw}, Title: fmt.Sprintf("Title %04d", count-i)}
					}
					caps := []FieldCapability{{Key: "value", ValueKind: kind, IndexedSortable: kind != "unindexed"}}
					specs := []viewconfig.SortSpec{{Field: "value", Direction: "desc"}, {Field: "title", Direction: "asc"}}
					rows := make([]TableRow, count)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						copy(rows, input)
						sortRows(rows, specs, caps)
					}
				})
			}
		}
	}
}
