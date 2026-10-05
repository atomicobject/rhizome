package views

import (
	"fmt"
	"testing"
)

func BenchmarkCollectCapabilityValues(b *testing.B) {
	for _, size := range []int{200, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			rows := make([]TableRow, size)
			caps := map[string]FieldCapability{"frontmatter.status": {}, "frontmatter.category": {}, "frontmatter.priority": {}}
			for i := range rows {
				rows[i] = testRow(fmt.Sprintf("%d.md", i), "Note", map[string]any{"status": fmt.Sprintf("status-%d", i), "category": fmt.Sprintf("category-%d", i), "priority": "normal"})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				collectCapabilityValues(caps, rows)
			}
		})
	}
}
