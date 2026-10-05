package views

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

func BenchmarkCapabilityGrouping(b *testing.B) {
	for _, fallback := range []bool{false, true} {
		b.Run(fmt.Sprintf("fallback=%t", fallback), func(b *testing.B) {
			rows := make([]TableRow, 1000)
			for i := range rows {
				status := []string{"open", "active", "done"}[i%3]
				fields := map[string]any{"frontmatter": map[string]any{"status": status}}
				if !fallback {
					fields["status"] = status
				}
				rows[i] = TableRow{Title: fmt.Sprintf("Task %d", i), Path: fmt.Sprintf("tasks/%d.md", i), ResolvedType: "Task", Fields: fields}
			}
			caps := []FieldCapability{{Key: "status", SourceKeys: []string{"status", "frontmatter.status"}, CanonicalField: "status"}}
			group := &viewconfig.GroupSpec{Field: "status"}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, groups := groupRows(rows, group, caps, false)
				if len(got) != 1000 || len(groups) != 3 {
					b.Fatal("unexpected grouping")
				}
			}
		})
	}
}

func TestCapabilityFieldValuePrecedence(t *testing.T) {
	cap := FieldCapability{Key: " key ", SourceKeys: []string{" key ", " source ", "source", "other"}, CanonicalField: " canonical "}
	tests := []struct {
		name   string
		fields map[string]any
		want   any
		ok     bool
	}{
		{"selector", map[string]any{"selected": "selected", "key": "key", "source": "source", "canonical": "canonical"}, "selected", true},
		{"key", map[string]any{"key": "key", "source": "source", "canonical": "canonical"}, "key", true},
		{"source order", map[string]any{"source": "source", "other": "other", "canonical": "canonical"}, "source", true},
		{"canonical", map[string]any{"canonical": "canonical"}, "canonical", true},
		{"nil stops fallback", map[string]any{"selected": nil, "key": "key"}, nil, true},
		{"empty stops fallback", map[string]any{"selected": "", "key": "key"}, "", true},
		{"missing", nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := capabilityFieldValue(TableRow{Fields: tt.fields}, " selected ", cap)
			if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got (%v, %t), want (%v, %t)", got, ok, tt.want, tt.ok)
			}
		})
	}
	t.Run("empty selectors and absent builtin", func(t *testing.T) {
		got, ok := capabilityFieldValue(TableRow{Fields: map[string]any{"fallback": false}}, " ", FieldCapability{Key: "title", SourceKeys: []string{"", " title ", "fallback"}})
		if !ok || got != false {
			t.Fatalf("got (%v, %t), want (false, true)", got, ok)
		}
	})
}
