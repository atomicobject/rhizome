package notemeta

import (
	"fmt"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

var metadataLegacyExportResult any

func BenchmarkMetadataLegacyExportNested(b *testing.B) {
	var object func(int) map[string]any
	object = func(depth int) map[string]any {
		if depth == 0 {
			return map[string]any{"name": "leaf", "count": 7, "flags": []any{true, false}}
		}
		out := make(map[string]any, 4)
		for index := range 4 {
			out[fmt.Sprintf("child%d", index)] = object(depth - 1)
		}
		return out
	}
	value, err := noteformat.NewMetadataValue(object(3))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		metadataLegacyExportResult = metadataValueLegacyExport(value)
	}
}
