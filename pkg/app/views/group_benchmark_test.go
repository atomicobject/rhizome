package views

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"testing"
)

func BenchmarkGroupRows(b *testing.B) {
	for _, count := range []int{10, 2048} {
		for _, buckets := range []int{1, 3, 100} {
			for _, repeated := range []bool{false, true} {
				b.Run(fmt.Sprintf("rows=%d/groups=%d/repeated=%t", count, buckets, repeated), func(b *testing.B) {
					rows := make([]TableRow, count)
					for i := range rows {
						n := (i * 1597) % buckets
						var value any = fmt.Sprintf("Project %03d", n)
						if repeated {
							value = []any{value, fmt.Sprintf("Project %03d", (n+1)%buckets), value}
						}
						rows[i] = TableRow{Title: fmt.Sprintf("Task %d", i), Path: fmt.Sprintf("tasks/%d.md", i), ResolvedType: "Task", Fields: map[string]any{"project": value, "frontmatter": map[string]any{"project": value}, "status": "active", "priority": 3}}
					}
					caps := []FieldCapability{{Key: "project", SourceKeys: []string{"project", "frontmatter.project"}, CanonicalField: "project"}}
					group := &viewconfig.GroupSpec{Field: "project"}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						groupRows(rows, group, caps, false)
					}
				})
			}
		}
	}
}

func BenchmarkGroupRowsEnum(b *testing.B) {
	for _, count := range []int{10, 2048} {
		b.Run(fmt.Sprintf("rows=%d", count), func(b *testing.B) {
			rows := make([]TableRow, count)
			values := []string{"open", "active", "done"}
			for i := range rows {
				rows[i] = TableRow{Title: fmt.Sprintf("Task %d", i), Path: fmt.Sprintf("tasks/%d.md", i), Fields: map[string]any{"status": values[i%3]}}
			}
			caps := []FieldCapability{{Key: "status", ValueKind: "enum", EnumValues: []FieldEnumValue{{Value: "open"}, {Value: "active"}, {Value: "done"}}}}
			group := &viewconfig.GroupSpec{Field: "status"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				groupRows(rows, group, caps, false)
			}
		})
	}
}
