package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseScalarPreservesAuthoredMeaning(t *testing.T) {
	for _, tc := range []struct {
		name, raw, canonical string
		kind                 ScalarKind
	}{
		{"Date", "2026/10/17", "2026-10-17", ScalarKindDate},
		{"DateTime", "2026-10-17T10:00:01.12-04:00", "2026-10-17T14:00:01.120000000Z", ScalarKindDateTime},
		{"Boolean", " TRUE ", "true", ScalarKindBoolean},
		{"Int", "+0042", "42", ScalarKindInt},
		{"Float", "1.500", "1.5", ScalarKindReal},
		{"String", " [[Path|Alias]] ", "[[Path|Alias]]", ScalarKindString},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, ok := ParseScalar(&Field{Kind: FieldKindScalar, TypeName: tc.name}, tc.raw)
			require.True(t, ok)
			require.Equal(t, tc.canonical, value.Canonical)
			require.Equal(t, tc.kind, value.Kind)
		})
	}
	for _, tc := range []struct{ name, raw string }{
		{"Float", "+Inf"}, {"Float", "-Inf"}, {"Float", "NaN"},
		{"Date", "2026-02-30"}, {"DateTime", "2026-10-17"},
		{"Int", "9223372036854775808"}, {"URL", "file:///tmp/note"},
	} {
		t.Run(tc.name+"/"+tc.raw, func(t *testing.T) {
			_, ok := ParseScalar(&Field{Kind: FieldKindScalar, TypeName: tc.name}, tc.raw)
			require.False(t, ok)
		})
	}
}
